package state

import (
	"os"
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

// Any setting agtop keeps in config.json can be set for one run from the
// environment: AGTOP_ and its JSON key in upper snake case, a nested one
// after its parent's, so copyOnSelect is AGTOP_COPY_ON_SELECT and
// hibernate.afterMinutes AGTOP_HIBERNATE_AFTER_MINUTES. Settings that are
// lists or maps can't be. A value from the environment isn't saved: what
// config.json held is written back in its place, unless you change the
// setting in agtop meanwhile.

// envSet is one setting the environment set.
type envSet struct {
	path []int         // its field, by index, from Config
	Name string        // the variable
	file reflect.Value // what config.json held
	env  reflect.Value // what the variable set
}

// EnvName is the variable that sets the setting at key, a JSON path
// ("copyOnSelect", "hibernate.afterMinutes").
func EnvName(key string) string {
	var b strings.Builder
	b.WriteString("AGTOP_")
	for i, r := range key {
		switch {
		case r == '.':
			b.WriteByte('_')
		case unicode.IsUpper(r) && i > 0 && key[i-1] != '.':
			b.WriteByte('_')
			b.WriteRune(r)
		default:
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	return b.String()
}

// applyEnv sets what the environment names on c, and returns what it set.
func applyEnv(c *Config, getenv func(string) (string, bool)) []envSet {
	var out []envSet
	var walk func(v reflect.Value, prefix string, path []int)
	walk = func(v reflect.Value, prefix string, path []int) {
		t := v.Type()
		for i := range t.NumField() {
			f := t.Field(i)
			key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if !f.IsExported() || key == "" || key == "-" {
				continue
			}
			fv, p := v.Field(i), append(append([]int(nil), path...), i)
			if fv.Kind() == reflect.Struct {
				walk(fv, prefix+key+".", p)
				continue
			}
			name := EnvName(prefix + key)
			raw, ok := getenv(name)
			if !ok {
				continue
			}
			set, ok := parseEnv(fv.Type(), strings.TrimSpace(raw))
			if !ok {
				continue
			}
			was := reflect.New(fv.Type()).Elem()
			was.Set(fv)
			fv.Set(set)
			out = append(out, envSet{path: p, Name: name, file: was, env: set})
		}
	}
	walk(reflect.ValueOf(c).Elem(), "", nil)
	return out
}

// parseEnv reads a variable's value as a setting of type t.
func parseEnv(t reflect.Type, s string) (reflect.Value, bool) {
	v := reflect.New(t).Elem()
	switch t.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		b, ok := parseBool(s)
		if !ok {
			return v, false
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return v, false
		}
		v.SetInt(n)
	case reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return v, false
		}
		v.SetFloat(f)
	case reflect.Pointer:
		if t.Elem().Kind() != reflect.Bool {
			return v, false
		}
		b, ok := parseBool(s)
		if !ok {
			return v, false
		}
		v.Set(reflect.ValueOf(&b))
	default:
		return v, false
	}
	return v, true
}

func parseBool(s string) (bool, bool) {
	switch strings.ToLower(s) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	}
	return false, false
}

// forSaving is c as config.json should keep it: each setting the
// environment set and you haven't changed since as the file had it.
func forSaving(c Config, env []envSet) Config {
	if len(env) == 0 {
		return c
	}
	v := reflect.ValueOf(&c).Elem()
	for _, e := range env {
		f := v.FieldByIndex(e.path)
		if reflect.DeepEqual(f.Interface(), e.env.Interface()) {
			f.Set(e.file)
		}
	}
	return c
}

// FromEnv is the variable that set the setting at key this run, if one did.
func (s *Store) FromEnv(key string) (string, bool) {
	name := EnvName(key)
	for _, e := range s.env {
		if e.Name == name {
			return name, true
		}
	}
	return "", false
}

// EnvBool is the bool setting at key as the environment sets it, if it
// does: for code that reads config.json itself.
func EnvBool(key string) (on, ok bool) {
	s, ok := lookupEnv(EnvName(key))
	if !ok {
		return false, false
	}
	return parseBool(strings.TrimSpace(s))
}

func lookupEnv(name string) (string, bool) { return os.LookupEnv(name) }
