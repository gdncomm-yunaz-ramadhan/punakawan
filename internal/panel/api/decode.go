package api

import (
	"bytes"
	"encoding/json"
	"io"
)

// decodeServerManaged decodes an HTTP request body into a strict protocol type
// (whose generated UnmarshalJSON rejects any body missing a schema-required
// field), first injecting the server-managed fields a client legitimately omits
// when it POSTs "the record minus server fields" (per plan §21/§37/§43). Each
// inject entry is applied only when the client did not meaningfully supply that
// field - i.e. the key is absent, JSON null, or an empty string - so a value
// the client does send is always kept. The store re-stamps version/timestamps
// afterwards, so injecting the schema version constant here is not a source of
// truth, only what lets the strict decode succeed.
func decodeServerManaged(body io.Reader, target any, inject map[string]any) error {
	raw, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
	}
	for k, v := range inject {
		if cur, ok := m[k]; !ok || isEmptyJSON(cur) {
			b, err := json.Marshal(v)
			if err != nil {
				return err
			}
			m[k] = b
		}
	}
	merged, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(merged, target)
}

// isEmptyJSON reports whether a raw JSON value is one a client did not
// meaningfully set: null or the empty string.
func isEmptyJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte(`""`))
}
