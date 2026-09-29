package main

// rush's plugin protocol: JSON-RPC 2.0 on fd 3, each message a 4-byte
// big-endian length and then that many bytes of JSON. Both sides send
// requests. See rush's plugins/skills/write-rush-plugin/references.

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// conn is the plugin's side of the channel to rush.
type conn struct {
	w   io.Writer
	wmu sync.Mutex

	pmu     sync.Mutex
	nextID  int
	pending map[string]chan message
}

func newConn(w io.Writer) *conn { return &conn{w: w, pending: map[string]chan message{}} }

// call asks rush, and decodes its answer into out (nil to drop it).
func (c *conn) call(method string, params, out any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	c.pmu.Lock()
	c.nextID++
	id := json.RawMessage(fmt.Sprint(c.nextID))
	ch := make(chan message, 1)
	c.pending[string(id)] = ch
	c.pmu.Unlock()
	if err := c.send(message{ID: id, Method: method, Params: p}); err != nil {
		return err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return errors.New(m.Error.Message)
		}
		if out == nil || len(m.Result) == 0 {
			return nil
		}
		return json.Unmarshal(m.Result, out)
	case <-time.After(90 * time.Second):
		c.pmu.Lock()
		delete(c.pending, string(id))
		c.pmu.Unlock()
		return fmt.Errorf("%s: rush didn't answer", method)
	}
}

// notify tells rush something, wanting no answer.
func (c *conn) notify(method string, params any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(message{Method: method, Params: p})
}

func (c *conn) send(m message) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	buf := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(buf, uint32(len(b)))
	copy(buf[4:], b)
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.w.Write(buf)
	return err
}

// serve reads what rush sends until it closes the channel: replies to
// our calls, its requests (answered concurrently) and its notifications
// (taken in order).
func (c *conn) serve(r io.Reader, handle func(method string, params json.RawMessage) (any, *rpcError)) error {
	br := bufio.NewReader(r)
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return err
		}
		n := binary.BigEndian.Uint32(hdr[:])
		if n > 16<<20 {
			return fmt.Errorf("a frame of %d bytes", n)
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(br, body); err != nil {
			return err
		}
		var m message
		if json.Unmarshal(body, &m) != nil {
			continue
		}
		switch {
		case m.Method == "":
			c.pmu.Lock()
			ch := c.pending[string(m.ID)]
			delete(c.pending, string(m.ID))
			c.pmu.Unlock()
			if ch != nil {
				ch <- m
			}
		case len(m.ID) == 0:
			handle(m.Method, m.Params)
		default:
			go func() {
				res, e := handle(m.Method, m.Params)
				if e == nil && res == nil {
					res = map[string]any{}
				}
				var raw json.RawMessage
				if e == nil {
					raw, _ = json.Marshal(res)
				}
				_ = c.send(message{ID: m.ID, Result: raw, Error: e})
			}()
		}
	}
}

func ipc() (*os.File, error) {
	f := os.NewFile(3, "rush")
	if f == nil {
		return nil, errors.New("run me from rush: I talk on fd 3")
	}
	return f, nil
}
