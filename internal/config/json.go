package config

import "encoding/json"

// jsonUnmarshal and jsonMarshalIndent name encoding/json for the mod files.
func jsonUnmarshal(body []byte, v any) error { return json.Unmarshal(body, v) }

func jsonMarshalIndent(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }
