package verify

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// expectedDigest resolves the digest res is to be held to.
//
// A resource that has no digest reports ok false with no error. A digest
// that is present but unusable is an error. Content that cannot be checked against
// the digest it claims must not pass as verified.
func expectedDigest(ctx context.Context, res *descriptor.Resource) (_ digest.Digest, ok bool, err error) {
	expected, err := parseDigest(res.Digest)
	switch {
	case err != nil:
		return "", false, fmt.Errorf("cannot verify resource %q against its digest: %w", res.ToIdentity(), err)
	case expected == "":
		slog.WarnContext(ctx, "resource carries no digest, so what the repository served cannot be verified",
			slog.Any("resource", res.ToIdentity()))
		return "", false, nil
	}

	return expected, true, nil
}

// Download verifies content with digest `res` declares, as it is read.
//
// It is only correct for a repository whose download returns exactly the bytes the
// digest was taken for. Where the digest covers something else, such as an OCI
// image resource whose digest is that of its manifest, use [Digest] instead.
func Download(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error) {
	expected, ok, err := expectedDigest(ctx, res)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return content, nil
	}

	verifying, err := NewBlob(content, expected)
	if err != nil {
		return nil, fmt.Errorf("cannot verify resource %q against its digest: %w", res.ToIdentity(), err)
	}

	return verifying, nil
}

// Digest compares a digest the repository has already determined to the one `res`
// declares, for content that is verified by identity rather than by hashing what
// was returned.
func Digest(ctx context.Context, res *descriptor.Resource, actual digest.Digest) error {
	expected, ok, err := expectedDigest(ctx, res)
	if err != nil || !ok {
		return err
	}

	if expected != actual {
		return fmt.Errorf("digest mismatch for resource %q: expected %s, got %s", res.ToIdentity(), expected, actual)
	}

	return nil
}
