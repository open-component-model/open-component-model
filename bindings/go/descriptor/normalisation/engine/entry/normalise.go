// Package entry implements the list-of-single-key-maps serialization used by
// the legacy jsonNormalisation/v1 algorithm.
//
// Unlike the JCS engine (RFC 8785, see package jcs), which canonicalises to a
// JSON object, this engine recursively rewrites every JSON object into a JSON
// array of single-key objects sorted by key. Arrays stay arrays; scalars and
// explicit nulls are left unchanged. The result is marshalled compactly
// (no spaces, no HTML escaping, UTF-8) so that sha256 over the bytes matches
// the digests produced by gardener's cc-utils (ocm/sign.py).
//
// The engine is deliberately OCM-agnostic: it operates on an arbitrary
// any tree (map[string]any, []any, scalars, explicit nil). Field selection
// and OCM-specific transforms live in the json/v1 algorithm package.
package entry

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
)

// Normalise serialises v into the list-of-single-key-maps form and marshals it
// to compact JSON bytes. Objects become arrays of single-key objects sorted by
// key (ascending, byte-wise); arrays stay arrays; scalars and explicit nulls
// are preserved.
func Normalise(v any) ([]byte, error) {
	return marshal(Transform(v))
}

// Transform recursively rewrites v into the list-of-single-key-maps form.
// A map[string]any becomes a []any of single-key map[string]any sorted by key.
// A []any keeps its order with each element transformed. Everything else
// (scalars, explicit nil) is returned unchanged.
func Transform(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := slices.Sorted(maps.Keys(t))
		out := make([]any, 0, len(keys))
		for _, k := range keys {
			out = append(out, map[string]any{k: Transform(t[k])})
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = Transform(t[i])
		}
		return out
	default:
		return v
	}
}

// marshal encodes an already-transformed tree to compact JSON without HTML
// escaping and without the trailing newline json.Encoder appends.
func marshal(v any) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
