package rfc2253

import (
	"encoding/asn1"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode/utf16"
)

// splitRFC2253 splits s on unescaped sep, aware of quotes.
// It preserves all backslashes; value-level decoding happens later.
func splitRFC2253(s string, sep byte) []string {
	b := []byte(s)
	var parts []string
	var buf []byte
	esc := false
	inQuotes := false

	for i := 0; i < len(b); i++ {
		c := b[i]

		if esc {
			buf = append(buf, '\\', c)
			esc = false
			continue
		}
		switch c {
		case '\\':
			esc = true
			continue
		case '"':
			inQuotes = !inQuotes
			buf = append(buf, c)
			continue
		}

		if c == sep && !inQuotes {
			parts = append(parts, string(buf))
			buf = buf[:0]
			continue
		}
		buf = append(buf, c)
	}
	if esc {
		buf = append(buf, '\\')
	}
	parts = append(parts, string(buf))
	return parts
}

// parseRFC2253 decodes RFC2253 escapes for a value and #hex BER.
func parseRFC2253(v string) string {
	// #hex BER form (RFC 2253 §2.4)
	if strings.HasPrefix(v, "#") {
		if s := decodeBER(v[1:]); s != "" {
			return s
		}
		// fall through to literal if invalid hex or BER
	}

	// quoted string: remove surrounding quotes
	if n := len(v); n >= 2 && v[0] == '"' && v[n-1] == '"' {
		v = v[1 : n-1]
	}

	in := []byte(v)
	var out strings.Builder
	out.Grow(len(in))

	for i := 0; i < len(in); i++ {
		c := in[i]
		if c != '\\' {
			out.WriteByte(c)
			continue
		}

		// hexpair \xx
		if i+2 < len(in) && isHex(in[i+1]) && isHex(in[i+2]) {
			if b, err := strconv.ParseUint(string(in[i+1:i+3]), 16, 8); err == nil {
				out.WriteByte(byte(b))
				i += 2
				continue
			}
		}

		// escaped space(s) at end → single space (RFC 2253 trailing space rule)
		if i+1 < len(in) && in[i+1] == ' ' {
			j := i + 1
			for j+1 < len(in) && in[j+1] == ' ' {
				j++
			}
			if j == len(in)-1 {
				out.WriteByte(' ')
				break
			}
		}

		// generic escape for special characters per RFC 2253: , + " \ < > ; and leading '#'/spaces
		if i+1 < len(in) {
			out.WriteByte(in[i+1])
			i++
		} else {
			// dangling backslash
			out.WriteByte('\\')
		}
	}
	return out.String()
}

func decodeBER(hexStr string) string {
	raw, err := hex.DecodeString(hexStr)
	if err != nil || len(raw) == 0 {
		return ""
	}
	var rv asn1.RawValue
	if _, err := asn1.Unmarshal(raw, &rv); err != nil {
		return ""
	}
	switch rv.Tag {
	case asn1.TagBMPString: // BMPString (UCS-2-BE)
		if len(rv.Bytes)%2 != 0 {
			return ""
		}
		u16 := make([]uint16, len(rv.Bytes)/2)
		for i := 0; i < len(u16); i++ {
			u16[i] = uint16(rv.Bytes[2*i])<<8 | uint16(rv.Bytes[2*i+1])
		}
		return string(utf16.Decode(u16))
	default:
		// Best effort: assume UTF-8 for other string-like tags.
		return string(rv.Bytes)
	}
}

func isHex(b byte) bool {
	return ('0' <= b && b <= '9') ||
		('a' <= b && b <= 'f') ||
		('A' <= b && b <= 'F')
}
