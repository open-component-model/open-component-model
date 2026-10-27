package repository

import (
	"context"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/verified"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// VerifyDownload verifies content with digest `res` contains, as it is read.
//
// The returned blob reports the digest, size and media type of content, not the
// digest the resource claims: digest processors establish a resource digest from
// it, and must not read back the claim they are checking. Every reader verifies
// independently: it errors when the content hashes to something else, and when
// it is only read in part. Verification is streaming, so a target written from a
// failing reader has to be removed by the caller.
//
// A resource that has no digest passes through unverified with a warning, but a
// digest that is present and unusable errors. Content that cannot be checked
// against the digest must not pass as verified.
func VerifyDownload(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error) {
	expected, err := ParseDigest(res.Digest)
	if err != nil {
		return nil, fmt.Errorf("failed to parse digest for resource %q: %w", res.Name, err)
	}
	if expected == "" {
		slog.WarnContext(ctx, "resource has no digest, no verification can be performed",
			slog.Any("resource", res.ToIdentity()))
		return content, nil
	}
	return &downloadedBlob{
		Blob:    verified.New(content, verified.Metadata{Digest: expected, Size: blob.SizeUnknown}),
		content: content,
	}, nil
}

// downloadedBlob is content verified against the resource digest that reports
// the digest of the content itself.
type downloadedBlob struct {
	*verified.Blob
	content blob.ReadOnlyBlob
}

// Digest returns the digest content reports, if any.
func (b *downloadedBlob) Digest() (string, bool) {
	if digestAware, ok := b.content.(blob.DigestAware); ok {
		return digestAware.Digest()
	}
	return "", false
}
