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
// newline json.Encoder appends is trimmed. Numbers are re-spelled to Python's
// json.dumps form (see pythoniseNumbers) before encoding.
func marshal(v any) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(pythoniseNumbers(v)); err != nil {
		return nil, err
	}
	return escapeNonASCII(bytes.TrimRight(buf.Bytes(), "\n")), nil
}

// pythoniseNumbers returns a copy of the tree with every number re-spelled to
// match Python's json.dumps, which is what cc-utils serialises with. Two number
// representations reach it:
//
//   - json.Number (the json/v1 algorithm decodes with UseNumber): an integer
//     token is emitted verbatim — this preserves full precision for big ints
//     that exceed float64, matching Python's arbitrary-precision int. A
//     fractional token is parsed and re-spelled via pythonFloatRepr.
//   - float64 (values constructed directly in Go, e.g. in tests): integral
//     values keep encoding/json's rendering (1.0 -> 1); fractional values are
//     re-spelled.
//
// NOTE: the json.Number spellings here DIVERGE from OCM v1, which decodes every
// number into float64 and emits Go's encoding/json spelling (big ints lose
// precision, 1e-05 renders as 0.00001). This engine deliberately matches
// cc-utils, the primary compatibility target; an OCM-v1-signed descriptor with
// numeric signing-label values will not verify.
func pythoniseNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = pythoniseNumbers(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = pythoniseNumbers(val)
		}
		return out
	case json.Number:
		s := string(t)
		if !strings.ContainsAny(s, ".eE") {
			return t // integer token: verbatim, full precision
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return t
		}
		return json.Number(pythonFloatRepr(f))
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
