package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Settings is an account's settings.json, edited in place. Keys agtop
// doesn't know about are kept exactly, and a save writes a temp file then
// renames it, because running Claude Code sessions read the file live.
type Settings struct {
	Path string
	raw  map[string]json.RawMessage
}

// LoadSettings reads acct's settings.json; a missing file is an empty one.
func LoadSettings(acct Account) (*Settings, error) {
	s := &Settings{Path: filepath.Join(acct.ConfigDir, "settings.json"), raw: map[string]json.RawMessage{}}
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s.raw); err != nil {
		return nil, err
	}
	return s, nil
}

// Get reads a value by dotted path, e.g. "permissions.defaultMode", into v.
// It reports whether the key is set.
func (s *Settings) Get(path string, v any) bool {
	raw, ok := s.lookup(strings.Split(path, "."))
	if !ok {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

// String is Get for a string, "" when unset.
func (s *Settings) String(path string) string {
	var v string
	s.Get(path, &v)
	return v
}

func (s *Settings) lookup(keys []string) (json.RawMessage, bool) {
	cur := s.raw
	for i, k := range keys {
		raw, ok := cur[k]
		if !ok {
			return nil, false
		}
		if i == len(keys)-1 {
			return raw, true
		}
		next := map[string]json.RawMessage{}
		if json.Unmarshal(raw, &next) != nil {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

// Set writes a value by dotted path; nil removes the key. Objects on the way
// are created, and their other keys kept.
func (s *Settings) Set(path string, v any) error {
	keys := strings.Split(path, ".")
	var val json.RawMessage
	if v != nil {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		val = b
	}
	s.raw = setIn(s.raw, keys, val)
	return nil
}

func setIn(m map[string]json.RawMessage, keys []string, val json.RawMessage) map[string]json.RawMessage {
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	k := keys[0]
	if len(keys) == 1 {
		if val == nil {
			delete(m, k)
		} else {
			m[k] = val
		}
		return m
	}
	child := map[string]json.RawMessage{}
	if raw, ok := m[k]; ok {
		_ = json.Unmarshal(raw, &child)
	}
	child = setIn(child, keys[1:], val)
	if len(child) == 0 {
		delete(m, k)
		return m
	}
	b, _ := json.Marshal(child)
	m[k] = b
	return m
}

// Env is the settings' env block: variables every session starts with.
func (s *Settings) Env() map[string]string {
	env := map[string]string{}
	s.Get("env", &env)
	return env
}

// SetEnv sets one variable; an empty value removes it.
func (s *Settings) SetEnv(name, value string) error {
	if value == "" {
		return s.Set("env."+name, nil)
	}
	return s.Set("env."+name, value)
}

// Save writes the file, indented as Claude Code writes it.
func (s *Settings) Save() error {
	b, err := json.MarshalIndent(s.raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".settings-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(s.Path); err == nil {
		mode = st.Mode().Perm()
	}
	_ = os.Chmod(tmp.Name(), mode)
	return os.Rename(tmp.Name(), s.Path)
}
