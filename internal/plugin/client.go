package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// BrokerSock is where the broker listens for agtop itself: session hosts
// and the CLI. Plugins can't reach it; their sandbox connects nowhere but
// their proxy.
func BrokerSock() string { return filepath.Join(Root(), "broker.sock") }

// BrokerLock is held by the running broker, so there is only ever one.
func BrokerLock() string { return filepath.Join(Root(), "broker.lock") }

// BrokerLog is where the broker writes what happened to its plugins.
func BrokerLog() string { return filepath.Join(Root(), "broker.log") }

// DialBroker connects to the running broker.
func DialBroker() (*Conn, error) {
	c, err := net.DialTimeout("unix", BrokerSock(), time.Second)
	if err != nil {
		return nil, err
	}
	return NewConn(c, nil), nil
}

// EnsureBroker starts the broker if any plugin is approved and it isn't
// running. It returns once the broker is starting, not started.
func EnsureBroker() error {
	if len(Approvals()) == 0 || Supported() != nil {
		return nil
	}
	if c, err := net.DialTimeout("unix", BrokerSock(), time.Second); err == nil {
		_ = c.Close()
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Root(), 0o700); err != nil {
		return err
	}
	lg, err := os.OpenFile(BrokerLog(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer lg.Close()
	cmd := exec.Command(exe, "plugind")
	cmd.Stdout, cmd.Stderr = lg, lg
	// Its own session, so it outlives whoever started it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// Broker is a session host's connection to the broker, made when first
// needed and again after it drops.
type Broker struct {
	mu   sync.Mutex
	conn *Conn
}

func (b *Broker) get(ctx context.Context) (*Conn, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil {
		select {
		case <-b.conn.Done():
			b.conn = nil
		default:
			return b.conn, nil
		}
	}
	started := false
	for {
		c, err := DialBroker()
		if err == nil {
			b.conn = c
			return c, nil
		}
		if !started {
			started = true
			if err := EnsureBroker(); err != nil {
				return nil, err
			}
		}
		select {
		case <-ctx.Done():
			return nil, errors.New("the plugin broker is not running")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// MCP passes one of Claude Code's MCP messages to a plugin and returns the
// plugin's reply, or an error reply of its own: Claude Code always gets an
// answer.
func (b *Broker) MCP(plugin, session string, msg json.RawMessage) json.RawMessage {
	var head struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	_ = json.Unmarshal(msg, &head)
	if len(head.ID) == 0 {
		// A notification: nothing to pass on, and Claude Code expects an
		// empty result.
		return json.RawMessage(`{"jsonrpc":"2.0","result":{}}`)
	}
	timeout := 30 * time.Second
	if head.Method == "tools/call" {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c, err := b.get(ctx)
	var res json.RawMessage
	if err == nil {
		res, err = c.CallRaw(ctx, "mcp", mustJSON(map[string]any{"plugin": plugin, "session": session, "message": msg}))
	}
	if err != nil {
		e := &Error{Code: CodeServer, Message: "agtop plugin " + plugin + ": " + err.Error()}
		if pe := (*Error)(nil); errors.As(err, &pe) {
			e.Code = pe.Code
		}
		return mustJSON(map[string]any{"jsonrpc": "2.0", "id": head.ID, "error": e})
	}
	return res
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
