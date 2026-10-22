package repositoryupload

import (
	"fmt"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// genericBlobDigestV1 is the normalisation algorithm of a digest of plain bytes.
const genericBlobDigestV1 = "genericBlobDigest/v1"

// hashAlgorithms maps the OCM hash algorithms of source digests to digest algorithms.
var hashAlgorithms = map[string]digest.Algorithm{"SHA-256": digest.SHA256, "SHA-512": digest.SHA512}

// expectedDigest returns the digest the uploaded content must have: the source digest, if it
// describes the uploaded bytes. It is empty when there is no source digest or the content was
// taken from an OCI artifact, whose digest describes a different byte representation.
func expectedDigest(src *descriptor.Digest, fromOCI bool) (digest.Digest, error) {
	if src == nil || fromOCI {
		return "", nil
	}
	alg, ok := hashAlgorithms[src.HashAlgorithm]
	if !ok {
		return "", fmt.Errorf("unsupported hash algorithm: expected SHA-256 or SHA-512, got %s", src.HashAlgorithm)
	}
	if src.NormalisationAlgorithm != genericBlobDigestV1 {
		return "", fmt.Errorf("unsupported normalisation algorithm: expected %s, got %s", genericBlobDigestV1, src.NormalisationAlgorithm)
	}
	d := digest.NewDigestFromEncoded(alg, src.Value)
	if err := d.Validate(); err != nil {
		return "", fmt.Errorf("invalid source digest: %w", err)
	}
	return d, nil
}

// knownDigest returns the digest the uploaded content must have (expected, see expectedDigest)
// and the one it is known to have up front (known): expected, else the digest the content
// reports itself. Both are empty when unknown.
func knownDigest(src *descriptor.Digest, content blob.ReadOnlyBlob, fromOCI bool) (expected, known digest.Digest, err error) {
	if expected, err = expectedDigest(src, fromOCI); err != nil {
		return "", "", err
	}
	if expected != "" {
		return expected, expected, nil
	}
	if da, ok := content.(blob.DigestAware); ok {
		if d, ok := da.Digest(); ok {
			if parsed, err := digest.Parse(d); err == nil {
				return "", parsed, nil
			}
		}
	}
	return "", "", nil
}

// uploadedDigest is the digest of the published resource: the source digest if it describes the
// uploaded content, else the digest of the uploaded bytes.
func uploadedDigest(src *descriptor.Digest, expected, uploaded digest.Digest) *descriptor.Digest {
	if expected != "" {
		return src.DeepCopy()
	}
	var hashAlgorithm string
	for name, alg := range hashAlgorithms {
		if alg == uploaded.Algorithm() {
			hashAlgorithm = name
		}
	}
	return &descriptor.Digest{HashAlgorithm: hashAlgorithm, NormalisationAlgorithm: genericBlobDigestV1, Value: uploaded.Encoded()}
}
