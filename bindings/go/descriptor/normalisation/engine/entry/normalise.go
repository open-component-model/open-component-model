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
// Descriptors reach this engine via the storage representation (sigs.k8s.io/yaml
// round-trip in json/v1), so object keys are already sorted and numbers already
// collapsed to float64; the engine only re-spells non-integral floats to match
// Python's json.dumps (see marshal/pythoniseFloats) and otherwise preserves
// order and values as received.
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
	"math"
	"slices"
	"strconv"
	"strings"
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
// newline json.Encoder appends is trimmed. Non-integral floats are re-spelled
// to match Python's repr (see pythoniseFloats) before encoding.
func marshal(v any) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(pythoniseFloats(v)); err != nil {
		return nil, err
	}
	return escapeNonASCII(bytes.TrimRight(buf.Bytes(), "\n")), nil
}

// pythoniseFloats returns a copy of the tree in which every non-integral
// float64 is replaced by a json.Number spelled the way Python's json.dumps
// (float repr) would render it. cc-utils serialises label values with
// json.dumps, and Go's encoding/json spells some fractional values differently
// (e.g. 1e-7 vs 1e-07, 0.00001 vs 1e-05), which would change the signed bytes.
// Integral floats are left untouched so encoding/json keeps its existing
// rendering (1.0 -> 1, large integral values unchanged); matching OCM v1, which
// also loads numbers via float64, that collapse is intentional and in scope of
// neither this fix nor the digests already verified on the branch.
func pythoniseFloats(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = pythoniseFloats(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = pythoniseFloats(val)
		}
		return out
	case float64:
		if t == math.Trunc(t) && !math.IsInf(t, 0) {
			return t
		}
		return json.Number(pythonFloatRepr(t))
	default:
		return v
	}
}

// pythonFloatRepr spells a non-integral float64 the way CPython's float repr
// (and therefore json.dumps) does: shortest round-tripping decimal, switching
// to exponential notation when the decimal point sits at position <= -4 or
// > 16, with an at-least-two-digit signed exponent. This is the spelling the
// cc-utils oracle emits for fractional signing-label values.
func pythonFloatRepr(f float64) string {
	s := strconv.FormatFloat(f, 'e', -1, 64) // e.g. "1e-07", "-1.25e+00"
	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	}
	ei := strings.IndexByte(s, 'e')
	digits := strings.Replace(s[:ei], ".", "", 1)
	exp, _ := strconv.Atoi(s[ei+1:])
	decpt := exp + 1 // position of the decimal point after the first digit

	var out string
	switch {
	case decpt <= -4 || decpt > 16:
		var b strings.Builder
		b.WriteByte(digits[0])
		if len(digits) > 1 {
			b.WriteByte('.')
			b.WriteString(digits[1:])
		}
		b.WriteByte('e')
		e := decpt - 1
		if e < 0 {
			b.WriteByte('-')
			e = -e
		} else {
			b.WriteByte('+')
		}
		es := strconv.Itoa(e)
		if len(es) < 2 {
			es = "0" + es
		}
		b.WriteString(es)
		out = b.String()
	case decpt <= 0:
		out = "0." + strings.Repeat("0", -decpt) + digits
	case decpt >= len(digits):
		out = digits + strings.Repeat("0", decpt-len(digits)) + ".0"
	default:
		out = digits[:decpt] + "." + digits[decpt:]
	}
	if neg {
		out = "-" + out
	}
	return out
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
