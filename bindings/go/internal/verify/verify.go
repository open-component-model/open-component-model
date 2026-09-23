package verify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Download verifies content with digest `res` contains, as it is read.
//
// A resource that has no digest passes through unverified with a warning, but a
// digest that is present and unusable is refused: content that cannot be checked
// against the digest it claims must not pass as verified.
func Download(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error) {
	expected, err := parseDigest(res.Digest)
	if err != nil {
		return nil, refuse(res, content, err)
	}
	if expected == "" {
		slog.WarnContext(ctx, "resource has no digest, no verification can be performed",
			slog.Any("resource", res.ToIdentity()))
		return content, nil
	}

	verifying, err := newVerifyingBlob(content, expected)
	if err != nil {
		return nil, refuse(res, content, err)
	}

	return verifying, nil
}

// refuse names the failure to verify res and releases content along the way, because
// the caller is handed nothing to close it with and the download has usually already
// put a temporary file on disk. Content that owns nothing is left alone.
func refuse(res *descriptor.Resource, content blob.ReadOnlyBlob, err error) error {
	err = fmt.Errorf("cannot verify resource %q against its digest: %w", res.ToIdentity(), err)

	closer, ok := content.(io.Closer)
	if !ok {
		return err
	}
	if closeErr := closer.Close(); closeErr != nil {
		return errors.Join(err, fmt.Errorf("failed to release unverifiable content: %w", closeErr))
	}

	return err
}
