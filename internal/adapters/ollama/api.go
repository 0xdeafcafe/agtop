package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// server is where Ollama listens: OLLAMA_HOST as Ollama itself reads it,
// else its own default.
func server() string {
	h := strings.TrimSpace(os.Getenv("OLLAMA_HOST"))
	if h == "" {
		return "http://127.0.0.1:11434"
	}
	if !strings.Contains(h, "://") {
		h = "http://" + h
	}
	if strings.HasPrefix(h, "http://0.0.0.0") {
		h = "http://127.0.0.1" + strings.TrimPrefix(h, "http://0.0.0.0")
	}
	if strings.Count(h, ":") < 2 { // no port
		h += ":11434"
	}
	return strings.TrimRight(h, "/")
}

// Model is what Ollama says of one of its models: /api/show, and /api/ps
// when it's loaded.
type Model struct {
	Name         string
	Capabilities []string
	Family       string
	Params       string // "31.1B"
	Quant        string // "Q4_K_M"
	MaxContext   int    // what the model was trained to
	Context      int    // what the server loaded it with; 0 when not loaded
	VRAM         int64
}

// Can is whether the model has capability c: tools, thinking, vision.
func (m Model) Can(c string) bool {
	for _, x := range m.Capabilities {
		if x == c {
			return true
		}
	}
	return false
}

// client has no timeout of its own: loading a model takes as long as it
// takes, so each caller bounds its own.
var client = &http.Client{}

func call(ctx context.Context, method, path string, body, out any) error {
	var r *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, server()+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("ollama: %s took too long: %w", path, ctx.Err())
		}
		return fmt.Errorf("Ollama isn't running at %s: start it with `ollama serve`, or open the Ollama app", server())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error string }
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("ollama: %s", e.Error)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// names are the models the server has.
func names(ctx context.Context) ([]string, error) {
	var tags struct {
		Models []struct{ Name string }
	}
	if err := call(ctx, http.MethodGet, "/api/tags", nil, &tags); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		out = append(out, m.Name)
	}
	return out, nil
}

// loaded are the models in memory now, with the context each was loaded
// with.
func loaded(ctx context.Context) (map[string]Model, error) {
	var ps struct {
		Models []struct {
			Name          string
			SizeVRAM      int64 `json:"size_vram"`
			ContextLength int   `json:"context_length"`
		}
	}
	if err := call(ctx, http.MethodGet, "/api/ps", nil, &ps); err != nil {
		return nil, err
	}
	out := map[string]Model{}
	for _, m := range ps.Models {
		out[m.Name] = Model{Name: m.Name, Context: m.ContextLength, VRAM: m.SizeVRAM}
	}
	return out, nil
}

// show is what the server knows of model name.
func show(ctx context.Context, name string) (Model, error) {
	var s struct {
		Capabilities []string
		Details      struct {
			Family            string
			ParameterSize     string `json:"parameter_size"`
			QuantizationLevel string `json:"quantization_level"`
		}
		ModelInfo map[string]any `json:"model_info"`
	}
	if err := call(ctx, http.MethodPost, "/api/show", map[string]string{"model": name}, &s); err != nil {
		return Model{}, err
	}
	m := Model{Name: name, Capabilities: s.Capabilities, Family: s.Details.Family,
		Params: s.Details.ParameterSize, Quant: s.Details.QuantizationLevel}
	for k, v := range s.ModelInfo {
		if strings.HasSuffix(k, ".context_length") {
			if f, ok := v.(float64); ok {
				m.MaxContext = int(f)
			}
		}
	}
	return m, nil
}

// load puts model name in memory, so the first turn doesn't wait on it
// and the context it's loaded with can be read back.
func load(ctx context.Context, name string) error {
	return call(ctx, http.MethodPost, "/api/generate", map[string]string{"model": name}, nil)
}
