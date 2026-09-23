package verify

import (
	"context"
	"errors"
	"fmt"
	"io"
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
		slog.WarnContext(ctx, "resource has no digest, no verification can be performed",
			slog.Any("resource", res.ToIdentity()))
		return "", false, nil
	}

	return expected, true, nil
}

// Download verifies content with digest `res` contains, as it is read.
//
// On error content is closed, because the caller is handed nothing to close it with
// and the download has usually already put a temporary file on disk.
func Download(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error) {
	expected, ok, err := expectedDigest(ctx, res)
	if err != nil {
		return nil, errors.Join(err, closeContent(content))
	}
	if !ok {
		return content, nil
	}

	verifying, err := NewBlob(content, expected)
	if err != nil {
		err = fmt.Errorf("cannot verify resource %q against its digest: %w", res.ToIdentity(), err)
		return nil, errors.Join(err, closeContent(content))
	}

	return verifying, nil
}

// closeContent releases a reader. If the blob is not a Closer, it will return nil.
func closeContent(content blob.ReadOnlyBlob) error {
	closer, ok := content.(io.Closer)
	if !ok {
		return nil
	}
	if err := closer.Close(); err != nil {
		return fmt.Errorf("failed to release unverifiable content: %w", err)
	}
	return nil
}
