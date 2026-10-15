// Package checksum implements the checksum-policy layer for wget-based inputs.
//
// A checksum policy lets the input method and access digest processor obtain
// and verify an expected checksum for a resource from its HTTP response
// headers (RFC 9530 Content-Digest and the x-checksum-* family). A stream
// source disables verification and computes the digest from the bytes.
//
// Verification and storage are decoupled: a policy may verify against any
// supported algorithm, but the OCM resource digest is always SHA-256 with the
// genericBlobDigest/v1 normalisation, so a weaker transport checksum never
// leaks into the descriptor.
package checksum

import (
	"crypto"
	"crypto/fips140"
	"hash"
	"strings"

	// Register the hash implementations behind crypto.Hash.New.
	_ "crypto/md5"  //nolint:gosec // MD5 is verification-only.
	_ "crypto/sha1" //nolint:gosec // SHA-1 is verification-only.
	_ "crypto/sha256"
	_ "crypto/sha512"
)

// Algorithm bridges the three naming schemes for a checksum algorithm: the
// OCM digest name, the RFC 9530 structured-field key, and the Maven external
// file extension.
type Algorithm struct {
	OCMName    string
	RFC9530Key string
	Extension  string
	Hash       crypto.Hash
}

// New returns a fresh hash.Hash for the algorithm.
//
// MD5 and SHA-1 are not FIPS-approved. They only verify checksums that remote
// servers publish; the digest OCM records and signs is always SHA-256. They
// therefore run outside strict enforcement, so verification keeps working with
// GODEBUG=fips140=only.
func (a Algorithm) New() hash.Hash {
	h := a.Hash.New()
	if a.Hash == crypto.MD5 || a.Hash == crypto.SHA1 {
		return unenforcedHash{h}
	}
	return h
}

// unenforcedHash runs every hashing call of a non-approved algorithm inside
// [fips140.WithoutEnforcement]. Outside fips140=only that is a direct call.
type unenforcedHash struct{ hash.Hash }

func (h unenforcedHash) Write(p []byte) (n int, err error) {
	fips140.WithoutEnforcement(func() { n, err = h.Hash.Write(p) })
	return n, err
}

func (h unenforcedHash) Sum(b []byte) (out []byte) {
	fips140.WithoutEnforcement(func() { out = h.Hash.Sum(b) })
	return out
}

var (
	// SHA256 is the algorithm OCM stores for wget blobs; always computed.
	SHA256 = Algorithm{OCMName: "SHA-256", RFC9530Key: "sha-256", Extension: "sha256", Hash: crypto.SHA256}
	// SHA512 is accepted for verification.
	SHA512 = Algorithm{OCMName: "SHA-512", RFC9530Key: "sha-512", Extension: "sha512", Hash: crypto.SHA512}
	// SHA1 is accepted for verification (common in Maven repositories).
	SHA1 = Algorithm{OCMName: "SHA-1", RFC9530Key: "sha", Extension: "sha1", Hash: crypto.SHA1}
	// MD5 is accepted for verification (legacy Maven repositories).
	MD5 = Algorithm{OCMName: "MD5", RFC9530Key: "md5", Extension: "md5", Hash: crypto.MD5}
)

// All lists every supported algorithm, strongest first.
var All = []Algorithm{SHA512, SHA256, SHA1, MD5}

// StorageAlgorithm is always computed and recorded on the OCM resource digest,
// regardless of which algorithm the policy verifies against.
var StorageAlgorithm = SHA256

// ByRFC9530Key returns the algorithm for an RFC 9530 dictionary key
// (case-insensitive).
func ByRFC9530Key(key string) (Algorithm, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, a := range All {
		if a.RFC9530Key == key {
			return a, true
		}
	}
	return Algorithm{}, false
}

// ByOCMName returns the algorithm for an OCM hash algorithm name
// (case-insensitive).
func ByOCMName(name string) (Algorithm, bool) {
	name = strings.ToUpper(strings.TrimSpace(name))
	for _, a := range All {
		if strings.ToUpper(a.OCMName) == name {
			return a, true
		}
	}
	return Algorithm{}, false
}
