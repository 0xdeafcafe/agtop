package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Settings is an account's settings.json, edited in place. Keys keep their
// order, keys agtop doesn't know about are kept exactly, and a save re-reads
// the file and applies only the changes made here, so whatever Claude Code
// wrote in the meantime survives. It writes a temp file then renames it
// (through a symlink, to the real file), because running Claude Code
// sessions read the file live.
type Settings struct {
	Path    string
	raw     *object
	changes []change
}

type change struct {
	keys []string
	val  json.RawMessage // nil removes
}

// object is a JSON object that remembers its key order.
type object struct {
	keys []string
	vals map[string]json.RawMessage
}

func newObject() *object { return &object{vals: map[string]json.RawMessage{}} }

func parseObject(b []byte) (*object, error) {
	o := newObject()
	if len(bytes.TrimSpace(b)) == 0 {
		return o, nil
	}
	d := json.NewDecoder(bytes.NewReader(b))
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	if t != json.Delim('{') {
		return nil, errors.New("settings.json isn't a JSON object")
	}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		k, _ := t.(string)
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return nil, err
		}
		if _, dup := o.vals[k]; !dup {
			o.keys = append(o.keys, k)
		}
		o.vals[k] = v
	}
	return o, nil
}

func (o *object) set(k string, v json.RawMessage) {
	if v == nil {
		if _, ok := o.vals[k]; ok {
			delete(o.vals, k)
			o.keys = slices.DeleteFunc(o.keys, func(x string) bool { return x == k })
		}
		return
	}
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *object) bytes() []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(encode(k))
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.Bytes()
}

// encode is json.Marshal without escaping <, > and &: a hook command like
// `a && b > out` must stay readable.
func encode(v any) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if e.Encode(v) != nil {
		return nil
	}
	return bytes.TrimRight(b.Bytes(), "\n")
}

// LoadSettings reads acct's settings.json; a missing file is an empty one.
func LoadSettings(acct Account) (*Settings, error) {
	return LoadSettingsFile(filepath.Join(acct.ConfigDir, "settings.json"))
}

// LoadSettingsFile reads any Claude Code settings file: a project's
// .claude/settings.json or settings.local.json. Missing is empty.
func LoadSettingsFile(path string) (*Settings, error) {
	s := &Settings{Path: path}
	raw, err := readObject(s.Path)
	if err != nil {
		return nil, err
	}
	s.raw = raw
	return s, nil
}

func readObject(path string) (*object, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return newObject(), nil
	}
	if err != nil {
		return nil, err
	}
	return parseObject(b)
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
		raw, ok := cur.vals[k]
		if !ok {
			return nil, false
		}
		if i == len(keys)-1 {
			return raw, true
		}
		next, err := parseObject(raw)
		if err != nil {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

// Set writes a value by dotted path; nil removes the key. Objects on the way
// are created, and their other keys kept in order.
func (s *Settings) Set(path string, v any) error {
	keys := strings.Split(path, ".")
	var val json.RawMessage
	if v != nil {
		if val = encode(v); val == nil {
			return errors.New("can't write that value")
		}
	}
	setIn(s.raw, keys, val)
	s.changes = append(s.changes, change{keys, val})
	return nil
}

func setIn(o *object, keys []string, val json.RawMessage) {
	k := keys[0]
	if len(keys) == 1 {
		o.set(k, val)
		return
	}
	child := newObject()
	if raw, ok := o.vals[k]; ok {
		if c, err := parseObject(raw); err == nil {
			child = c
		}
	}
	setIn(child, keys[1:], val)
	if len(child.keys) == 0 {
		o.set(k, nil)
		return
	}
	o.set(k, child.bytes())
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

// Save applies the changes made here to the file as it is now, indented as
// Claude Code writes it.
func (s *Settings) Save() error {
	path := s.Path
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real // a dotfiles symlink stays a symlink
	}
	cur, err := readObject(path)
	if err != nil {
		return err
	}
	for _, c := range s.changes {
		setIn(cur, c.keys, c.val)
	}
	var b bytes.Buffer
	if err := json.Indent(&b, cur.bytes(), "", "  "); err != nil {
		return err
	}
	b.WriteByte('\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	_ = os.Chmod(tmp.Name(), mode)
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	s.raw, s.changes = cur, nil
	return nil
}
