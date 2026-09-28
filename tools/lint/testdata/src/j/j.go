package j

import (
	"encoding/json" // want `encoding/json is json v1`
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"strings"
)

var _ jsontext.Value // the types are fine

func f(b []byte, v any) {
	_ = json.Unmarshal(b, v)
	_, _ = jsonv2.Marshal(v)                       // want `call jsonx rather than v2.Marshal`
	_ = jsonv2.Unmarshal(b, v)                     // want `call jsonx rather than v2.Unmarshal`
	_ = jsontext.NewDecoder(strings.NewReader("")) // want `call jsonx rather than jsontext.NewDecoder`
}
