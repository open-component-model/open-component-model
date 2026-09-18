package checksum

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

// Expected is a checksum obtained from a policy source: the algorithm it was
// computed with and its lowercase hex value.
type Expected struct {
	Algorithm Algorithm
	// Value is the lowercase hex-encoded checksum.
	Value string
}

// FromHeaders extracts expected checksums from HTTP response headers, trying the
// standardized RFC 9530 Content-Digest field first, then the non-standard
// x-checksum-* family used by Maven Central, Google, and AWS S3 mirrors.
//
// Only algorithms in [All] are returned. When several are present the caller
// selects which to verify against via [Select].
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

// parseContentDigest parses RFC 9530 Content-Digest field values. Each value is a
// structured-field Dictionary whose members are `key=:base64:`, where key is a
// hash-algorithm key and the value is a Byte Sequence (base64) of the raw digest.
// Repr-Digest is intentionally ignored: it is computed over the representation
// (affected by content coding), whereas we persist the transferred content bytes,
// for which Content-Digest is the correct field.
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
			// Structured-field Byte Sequences are wrapped in colons: :base64:.
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

// splitDictionary splits a structured-field Dictionary into its member segments,
// honoring the colon-delimited Byte Sequence syntax so a comma inside a base64
// value's surrounding colons is not treated as a member separator. Base64 never
// contains a colon, so toggling on ':' is sufficient.
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

// legacyChecksumHeaders are the non-standard response headers that repositories
// use to convey checksums, mapped to their algorithm. These are checked in
// addition to any caller-supplied extra header names.
var legacyChecksumHeaders = map[string]Algorithm{
	"x-checksum-sha256":           SHA256,
	"x-checksum-sha512":           SHA512,
	"x-checksum-sha1":             SHA1,
	"x-checksum-md5":              MD5,
	"x-goog-meta-checksum-sha256": SHA256,
	"x-goog-meta-checksum-sha1":   SHA1,
	"x-goog-meta-checksum-md5":    MD5,
	"x-amz-meta-checksum-sha256":  SHA256,
	"x-amz-meta-checksum-sha1":    SHA1,
	"x-amz-meta-checksum-md5":     MD5,
}

// parseLegacyChecksumHeaders reads hex checksums from the well-known x-checksum-*
// headers plus any caller-supplied extra header names. An extra header name may be
// suffixed with the algorithm ("x-my-sha256"); otherwise the algorithm is inferred
// from a known suffix.
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
	for name, alg := range legacyChecksumHeaders {
		take(name, alg)
	}
	for _, name := range extra {
		if alg, ok := algorithmFromHeaderName(name); ok {
			take(name, alg)
		}
	}
	return out
}

// algorithmFromHeaderName infers the algorithm from a header name's trailing
// algorithm token (e.g. "x-artifact-sha256" -> SHA256).
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

// Select picks the strongest expected checksum whose algorithm is in prefer
// (or, when prefer is empty, the strongest supported). ok is false when none of
// the candidates match.
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

// Verify compares a computed hex digest for the expected algorithm against the
// expected value. The computed map is keyed by algorithm OCM name. A missing
// computed entry for the expected algorithm is an error, since the caller is
// responsible for computing every algorithm a policy may require.
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
