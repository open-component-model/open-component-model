package checksum

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

// Expected is a checksum obtained from a policy source.
type Expected struct {
	Algorithm Algorithm
	// Value is the lowercase hex-encoded checksum.
	Value string
}

// FromHeaders extracts expected checksums from response headers, trying RFC
// 9530 Content-Digest first, then the non-standard x-checksum-* family used by
// Maven Central and cloud object stores.
func FromHeaders(header http.Header, extra []string) []Expected {
	var out []Expected
	seen := map[string]struct{}{}
	add := func(e Expected) {
		key := e.Algorithm.OCMName + ":" + e.Value
		if _, dup := seen[key]; dup || e.Value == "" {
			return
		}
		seen[key] = struct{}{}
		out = append(out, e)
	}

	for _, e := range parseContentDigest(header.Values("Content-Digest")) {
		add(e)
	}
	for _, e := range parseLegacyChecksumHeaders(header, extra) {
		add(e)
	}
	return out
}

// parseContentDigest parses RFC 9530 Content-Digest fields. Each is a
// structured-field Dictionary of `key=:base64:`. Repr-Digest is intentionally
// ignored: it is computed over the representation, whereas we persist the
// transferred content bytes.
func parseContentDigest(values []string) []Expected {
	var out []Expected
	for _, value := range values {
		for _, member := range splitDictionary(value) {
			key, raw, ok := strings.Cut(member, "=")
			if !ok {
				continue
			}
			alg, known := ByRFC9530Key(strings.TrimSpace(key))
			if !known {
				continue
			}
			b64 := strings.TrimSpace(raw)
			b64 = strings.TrimPrefix(b64, ":")
			b64 = strings.TrimSuffix(b64, ":")
			decoded, err := base64.StdEncoding.DecodeString(b64)
			if err != nil || len(decoded) != alg.Hash.Size() {
				continue
			}
			out = append(out, Expected{Algorithm: alg, Value: hex.EncodeToString(decoded)})
		}
	}
	return out
}

// splitDictionary splits a structured-field Dictionary into its members,
// honoring `:base64:` Byte Sequences so a comma inside colons is not treated
// as a member separator. Base64 never contains a colon.
func splitDictionary(value string) []string {
	var members []string
	var current strings.Builder
	inByteSeq := false
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c == ':':
			inByteSeq = !inByteSeq
			current.WriteByte(c)
		case c == ',' && !inByteSeq:
			members = append(members, current.String())
			current.Reset()
		default:
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		members = append(members, current.String())
	}
	return members
}

type legacyChecksumHeader struct {
	Name      string
	Algorithm Algorithm
}

// legacyChecksumHeaders lists the non-standard response headers repositories
// use to convey checksums, ordered strongest-first per family so FromHeaders'
// output is deterministic when several are present.
var legacyChecksumHeaders = []legacyChecksumHeader{
	{"x-checksum-sha512", SHA512},
	{"x-checksum-sha256", SHA256},
	{"x-checksum-sha1", SHA1},
	{"x-checksum-md5", MD5},
	{"x-goog-meta-checksum-sha256", SHA256},
	{"x-goog-meta-checksum-sha1", SHA1},
	{"x-goog-meta-checksum-md5", MD5},
	{"x-amz-meta-checksum-sha256", SHA256},
	{"x-amz-meta-checksum-sha1", SHA1},
	{"x-amz-meta-checksum-md5", MD5},
}

// parseLegacyChecksumHeaders reads hex checksums from x-checksum-* headers
// plus caller-supplied extras. An extra header name is expected to carry a
// trailing algorithm token ("x-my-sha256").
func parseLegacyChecksumHeaders(header http.Header, extra []string) []Expected {
	var out []Expected
	take := func(name string, alg Algorithm) {
		v := strings.TrimSpace(header.Get(name))
		if v == "" {
			return
		}
		// Some servers quote the value or prefix it with the algorithm.
		v = strings.Trim(v, `"`)
		if _, rest, ok := strings.Cut(v, ":"); ok {
			v = strings.TrimSpace(rest)
		}
		v = strings.ToLower(v)
		if !isHex(v, alg.Hash.Size()) {
			return
		}
		out = append(out, Expected{Algorithm: alg, Value: v})
	}
	for _, h := range legacyChecksumHeaders {
		take(h.Name, h.Algorithm)
	}
	for _, name := range extra {
		if alg, ok := algorithmFromHeaderName(name); ok {
			take(name, alg)
		}
	}
	return out
}

// algorithmFromHeaderName infers the algorithm from a trailing token
// ("x-artifact-sha256" -> SHA256).
func algorithmFromHeaderName(name string) (Algorithm, bool) {
	lower := strings.ToLower(name)
	for _, a := range All {
		if strings.HasSuffix(lower, a.Extension) {
			return a, true
		}
	}
	return Algorithm{}, false
}

// isHex reports whether s is lowercase hex encoding exactly size bytes.
func isHex(s string, size int) bool {
	if len(s) != size*2 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// Select picks the strongest candidate whose algorithm appears in prefer.
// Empty prefer means [All].
func Select(candidates []Expected, prefer []Algorithm) (Expected, bool) {
	order := prefer
	if len(order) == 0 {
		order = All
	}
	for _, alg := range order {
		for _, c := range candidates {
			if c.Algorithm.OCMName == alg.OCMName {
				return c, true
			}
		}
	}
	return Expected{}, false
}

// Verify compares the computed digest against expected. A missing computed
// entry is an error: the caller is responsible for computing every algorithm
// a policy may require.
func Verify(computed map[string]string, expected Expected) error {
	got, ok := computed[expected.Algorithm.OCMName]
	if !ok {
		return fmt.Errorf("no computed %s digest available to verify against", expected.Algorithm.OCMName)
	}
	if !strings.EqualFold(got, expected.Value) {
		return fmt.Errorf("%s checksum mismatch: expected %s, computed %s", expected.Algorithm.OCMName, expected.Value, got)
	}
	return nil
}
