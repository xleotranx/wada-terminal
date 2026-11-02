package core

import "encoding/json"

// jsonMarshal and jsonUnmarshal name encoding/json so the protocol files read
// as protocol, not as plumbing.
func jsonMarshal(v any) ([]byte, error)   { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
