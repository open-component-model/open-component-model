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

// TransformRaw applies the same normalise_obj semantics as Transform but on raw
// JSON bytes, so that source key order survives for values cc-utils emits
// without recursing. Objects are rewritten into sorted single-key arrays
// (recursing per value); arrays and scalars are decoded order-preservingly and
// re-encoded canonically (escaping normalised, object key order kept). cc-utils
// never descends into arrays, so an object nested inside an array keeps its
// source key order instead of being re-sorted by Go's map marshalling. Use this
// for label values, whose structure is caller-defined.
func TransformRaw(raw json.RawMessage) (any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '{' {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return nil, err
		}
		out := make([]any, 0, len(obj))
		for _, k := range slices.Sorted(maps.Keys(obj)) {
			v, err := TransformRaw(obj[k])
			if err != nil {
				return nil, err
			}
			out = append(out, map[string]any{k: v})
		}
		return out, nil
	}
	return decodeOrdered(trimmed)
}

// orderedObject is a JSON object that marshals its members in insertion order
// (not sorted), without HTML escaping. It carries the source key order of a
// JSON object that cc-utils leaves inside a raw array.
type orderedObject []orderedMember

type orderedMember struct {
	key string
	val any
}

func (o orderedObject) MarshalJSON() ([]byte, error) {
	buf := new(bytes.Buffer)
	buf.WriteByte('{')
	for i := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := encodeNoHTML(buf, o[i].key); err != nil {
			return nil, err
		}
		buf.WriteByte(':')
		if err := encodeNoHTML(buf, o[i].val); err != nil {
			return nil, err
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func encodeNoHTML(buf *bytes.Buffer, v any) error {
	tmp := new(bytes.Buffer)
	enc := json.NewEncoder(tmp)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	buf.Write(bytes.TrimRight(tmp.Bytes(), "\n"))
	return nil
}

// decodeOrdered decodes JSON into a tree that preserves object key order
// (orderedObject for objects, []any for arrays, json.Number for numbers). It is
// used for values cc-utils emits raw, where source order is significant.
func decodeOrdered(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := decodeOrderedValue(dec)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func decodeOrderedValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil // string, json.Number, bool, nil
	}
	switch delim {
	case '{':
		obj := orderedObject{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			val, err := decodeOrderedValue(dec)
			if err != nil {
				return nil, err
			}
			obj = append(obj, orderedMember{key: keyTok.(string), val: val})
		}
		if _, err := dec.Token(); err != nil { // consume '}'
			return nil, err
		}
		return obj, nil
	case '[':
		arr := []any{}
		for dec.More() {
			val, err := decodeOrderedValue(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, val)
		}
		if _, err := dec.Token(); err != nil { // consume ']'
			return nil, err
		}
		return arr, nil
	}
	return tok, nil
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
