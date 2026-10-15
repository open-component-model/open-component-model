// Package entry implements the list-of-single-key-maps serialization used by
// the legacy jsonNormalisation/v1 algorithm.
//
// Unlike the JCS engine (RFC 8785, see package jcs), which canonicalises to a
// JSON object, this engine recursively rewrites every JSON object into a JSON
// array of single-key objects sorted by key. Values that are themselves
// objects are recursed into; arrays and scalars are left untouched — mirroring
// cc-utils' normalise_obj, which only descends into dicts. The OCM-specific
// lists (resources, componentReferences, labels) are normalised element-wise by
// the json/v1 algorithm before they reach this engine. The result is marshalled
// compactly (no spaces, HTML characters &<> kept literal, non-ASCII escaped to
// \uXXXX) so that sha256 over the bytes matches the digests produced by
// gardener's cc-utils (ocm/sign.py, which serialises with Python json.dumps).
//
// The engine is deliberately OCM-agnostic: it operates on an arbitrary
// any tree (map[string]any, []any, scalars, explicit nil). Field selection
// and OCM-specific transforms live in the json/v1 algorithm package.
package entry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"unicode/utf16"
	"unicode/utf8"
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
// Values that are objects are recursed into; arrays and scalars (including
// explicit nil) are returned unchanged. This mirrors cc-utils' normalise_obj,
// which descends into dicts only: a []any is never rewritten here, so callers
// must transform the OCM element lists (resources, componentReferences, labels)
// themselves before handing the tree to this engine.
func Transform(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := slices.Sorted(maps.Keys(t))
		out := make([]any, 0, len(keys))
		for _, k := range keys {
			out = append(out, map[string]any{k: Transform(t[k])})
		}
		return out
	default:
		return v
	}
}

// marshal encodes an already-transformed tree to compact JSON. HTML escaping
// is disabled (cc-utils keeps &<> literal) but non-ASCII runes are escaped to
// \uXXXX to match Python json.dumps' default (ensure_ascii=True); the trailing
// newline json.Encoder appends is trimmed.
func marshal(v any) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return escapeNonASCII(bytes.TrimRight(buf.Bytes(), "\n")), nil
}

// escapeNonASCII rewrites every rune >= 0x80 as a lowercase \uXXXX escape
// (surrogate pair above the BMP), mirroring cc-utils' json.dumps default
// (ensure_ascii=True). All structural JSON bytes are ASCII, so scanning the
// whole buffer is safe. Pure-ASCII input (the common case) is returned as-is.
func escapeNonASCII(b []byte) []byte {
	if utf8.RuneCount(b) == len(b) {
		return b
	}
	out := new(bytes.Buffer)
	out.Grow(len(b) + 8)
	for i := 0; i < len(b); {
		if b[i] < utf8.RuneSelf {
			out.WriteByte(b[i])
			i++
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		i += size
		if r > 0xFFFF {
			r1, r2 := utf16.EncodeRune(r)
			fmt.Fprintf(out, `\u%04x\u%04x`, r1, r2)
		} else {
			fmt.Fprintf(out, `\u%04x`, r)
		}
	}
	return out.Bytes()
}
