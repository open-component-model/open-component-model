package repository

import (
	"context"
	"fmt"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// expectedDigest resolves the digest res is to be held to.
//
// A resource carrying no digest reports ok false with no error, because resources
// added with --skip-reference-digest-processing legitimately have none. A digest
// that is present but unusable is an error: content that cannot be checked against
// the digest it claims must not pass as verified.
func expectedDigest(res *descriptor.Resource) (_ digest.Digest, ok bool, err error) {
	expected, err := res.Digest.Parse()
	switch {
	case err != nil:
		return "", false, fmt.Errorf("cannot verify resource %q against its digest: %w", res.ToIdentity(), err)
	case expected == "":
		return "", false, nil
	}

	return expected, true, nil
}

// NewVerifyingBlob wraps content so that the caller can hold it to the digest res
// declares, by calling [blob.VerifyingBlob.Verify] when it knows whether it wants
// verification.
//
// A resource carrying no digest is not an error: it yields a blob with nothing to
// verify, and Verify reports that. A digest that is present but unusable is an
// error here rather than from Verify, so that a download does not appear to succeed
// and then fail later.
//
// It is only correct for a repository whose download returns exactly the bytes the
// digest was taken over. Where the digest covers something else, such as an OCI
// image resource whose digest is that of its manifest, use [VerifyDigest] instead.
func NewVerifyingBlob(res *descriptor.Resource, content blob.ReadOnlyBlob) (*blob.VerifyingBlob, error) {
	expected, err := res.Digest.Parse()
	if err != nil {
		return nil, fmt.Errorf("cannot verify resource %q against its digest: %w", res.ToIdentity(), err)
	}

	// An empty digest carries through: the blob has nothing to verify.
	verifying, err := blob.NewVerifyingBlob(content, expected)
	if err != nil {
		return nil, fmt.Errorf("cannot verify resource %q against its digest: %w", res.ToIdentity(), err)
	}

	return verifying, nil
}

// VerifyDigest holds a digest the repository has already determined to the one res
// declares, for content that is verified by identity rather than by hashing what
// was returned: an OCI manifest resolved for an access, or the archive a helm chart
// was packed from.
func VerifyDigest(_ context.Context, res *descriptor.Resource, actual digest.Digest) error {
	expected, ok, err := expectedDigest(res)
	if err != nil || !ok {
		return err
	}

	if expected != actual {
		return fmt.Errorf("digest mismatch for resource %q: expected %s, got %s", res.ToIdentity(), expected, actual)
	}

	return nil
}
