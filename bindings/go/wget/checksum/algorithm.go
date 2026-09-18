// Package checksum implements the checksum-policy layer for wget-based inputs.
//
// When a wget input provides no digest of its own, a checksum policy lets the
// input method obtain and verify an expected checksum for the downloaded bytes
// from different sources, modelled on Maven's expected-checksum strategies
// (https://maven.apache.org/resolver/expected-checksums.html):
//
//   - httpHeader ("Remote Included"): the checksum travels in the download
//     response headers. Both the IETF-standard RFC 9530 Content-Digest field and
//     the widely-used non-standard x-checksum-* headers are understood.
//   - externalUrl ("Remote External"): the checksum is a sibling resource fetched
//     from a separate URL (e.g. <url>.sha256), as used by Maven repositories.
//   - stream: no expected checksum; the digest is computed from the stream.
//
// Verification and storage are deliberately decoupled. A policy may verify the
// transferred bytes against any supported algorithm (Maven commonly ships SHA-1
// or MD5), but the digest recorded on the OCM resource is always SHA-256 with the
// genericBlobDigest/v1 normalisation, so a non-SHA-256 transport checksum never
// leaks a weak or non-canonical algorithm into the component descriptor or OCI
// storage. See [Algorithm] for the supported set.
package checksum

import (
	"crypto"
	"fmt"
	"strings"

	// Register the hash implementations behind crypto.Hash.New used by this package.
	_ "crypto/md5"  //nolint:gosec // MD5 is verification-only, never used for OCM digests.
	_ "crypto/sha1" //nolint:gosec // SHA-1 is verification-only, never used for OCM digests.
	_ "crypto/sha256"
	_ "crypto/sha512"
)

// Algorithm identifies a checksum algorithm across the three naming schemes this
// package bridges: the OCM digest name (as stored on descriptor.Digest), the
// RFC 9530 structured-field key, and the Maven external-checksum file extension.
type Algorithm struct {
	// OCMName is the canonical OCM hash algorithm name (e.g. "SHA-256"), matching
	// descriptor.Digest.HashAlgorithm.
	OCMName string
	// RFC9530Key is the lowercase algorithm key used in RFC 9530 Content-Digest
	// dictionary members (e.g. "sha-256").
	RFC9530Key string
	// Extension is the file extension (without a dot) used by Maven "Remote
	// External" checksum files (e.g. "sha256").
	Extension string
	// Hash is the crypto.Hash used to compute the checksum.
	Hash crypto.Hash
}

// New returns a fresh hash.Hash for the algorithm.
func (a Algorithm) New() interface{ Write([]byte) (int, error) } { //nolint:ireturn // returns hash.Hash intentionally.
	return a.Hash.New()
}

var (
	// SHA256 is the algorithm OCM stores for wget blobs; it is always computed.
	SHA256 = Algorithm{OCMName: "SHA-256", RFC9530Key: "sha-256", Extension: "sha256", Hash: crypto.SHA256}
	// SHA512 is accepted for verification (RFC 9530 and external checksums).
	SHA512 = Algorithm{OCMName: "SHA-512", RFC9530Key: "sha-512", Extension: "sha512", Hash: crypto.SHA512}
	// SHA1 is accepted for verification only (common in Maven repositories).
	SHA1 = Algorithm{OCMName: "SHA-1", RFC9530Key: "sha", Extension: "sha1", Hash: crypto.SHA1}
	// MD5 is accepted for verification only (legacy Maven repositories).
	MD5 = Algorithm{OCMName: "MD5", RFC9530Key: "md5", Extension: "md5", Hash: crypto.MD5}
)

// All lists every supported algorithm, strongest first. Verification prefers the
// earliest match when a source offers several.
var All = []Algorithm{SHA512, SHA256, SHA1, MD5}

// StorageAlgorithm is the algorithm always computed and recorded on the OCM
// resource digest, independent of which algorithm a policy verifies against.
var StorageAlgorithm = SHA256

// ByRFC9530Key returns the algorithm for an RFC 9530 dictionary key
// (case-insensitive). ok is false when the key is not supported.
func ByRFC9530Key(key string) (Algorithm, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, a := range All {
		if a.RFC9530Key == key {
			return a, true
		}
	}
	return Algorithm{}, false
}

// ByExtension returns the algorithm for a Maven external-checksum file extension
// (case-insensitive, leading dot tolerated). ok is false when unsupported.
func ByExtension(ext string) (Algorithm, bool) {
	ext = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(ext, ".")))
	for _, a := range All {
		if a.Extension == ext {
			return a, true
		}
	}
	return Algorithm{}, false
}

// ByOCMName returns the algorithm for an OCM hash algorithm name
// (case-insensitive). ok is false when unsupported.
func ByOCMName(name string) (Algorithm, bool) {
	name = strings.ToUpper(strings.TrimSpace(name))
	for _, a := range All {
		if strings.ToUpper(a.OCMName) == name {
			return a, true
		}
	}
	return Algorithm{}, false
}

// AlgorithmsFromExtensions resolves a list of file extensions to algorithms,
// preserving order and rejecting unknown extensions.
func AlgorithmsFromExtensions(exts []string) ([]Algorithm, error) {
	out := make([]Algorithm, 0, len(exts))
	for _, ext := range exts {
		a, ok := ByExtension(ext)
		if !ok {
			return nil, fmt.Errorf("unsupported checksum algorithm %q", ext)
		}
		out = append(out, a)
	}
	return out, nil
}
