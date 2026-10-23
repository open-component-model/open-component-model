package shared

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocilayout "ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/repository"
)

const (
	genericBlobDigestV1 = "genericBlobDigest/v1"
	ociArtifactDigestV1 = "ociArtifactDigest/v1"
)

// VerifyResourceContent binds downloaded content to the digest of res.
//
// Downloads re-read the component version by tag, so a tag that moves between reading
// the descriptor and reading the blob could otherwise substitute content. Checking
// against res, the resource of the descriptor the caller resolved (and pinned), closes
// that gap. Blob digests (genericBlobDigest/v1) are verified while the returned blob is
// read; OCI artifacts (ociArtifactDigest/v1, delivered as an OCI layout) are verified
// up front: the single main artifact must have the resource digest and every blob of
// its graph must match its descriptor. Resources without a digest, or excluded from
// signing, are not covered by the component version digest and pass with a warning.
func VerifyResourceContent(ctx context.Context, res *descriptor.Resource, data blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error) {
	d := res.Digest
	if d == nil || d.Value == "" ||
		strings.EqualFold(d.HashAlgorithm, descriptor.NoDigest) ||
		strings.EqualFold(d.NormalisationAlgorithm, descriptor.ExcludeFromSignature) {
		return repository.VerifyDownload(ctx, res, data)
	}

	switch {
	case d.NormalisationAlgorithm == ociArtifactDigestV1,
		d.NormalisationAlgorithm == genericBlobDigestV1 && isOCILayout(data):
		// ociArtifactDigest/v1 is the manifest digest; descriptors written by older
		// versions label it genericBlobDigest/v1, recognisable by the layout media type.
		if err := verifyOCILayout(ctx, data, d); err != nil {
			return nil, fmt.Errorf("verifying resource %q against digest failed: %w", res.Name, err)
		}
		return data, nil
	case d.NormalisationAlgorithm == genericBlobDigestV1:
		return repository.VerifyDownload(ctx, res, data)
	default:
		return nil, fmt.Errorf("cannot verify resource %q: unsupported digest normalisation algorithm %q", res.Name, d.NormalisationAlgorithm)
	}
}

func isOCILayout(data blob.ReadOnlyBlob) bool {
	aware, ok := data.(blob.MediaTypeAware)
	if !ok {
		return false
	}
	mediaType, ok := aware.MediaType()
	return ok && ocilayout.IsOCILayoutMediaType(mediaType)
}

func verifyOCILayout(ctx context.Context, data blob.ReadOnlyBlob, expected *descriptor.Digest) (err error) {
	want, err := repository.ParseDigest(expected)
	if err != nil {
		return err
	}
	store, err := ocitar.ReadOCILayout(ctx, data)
	if err != nil {
		return fmt.Errorf("reading OCI layout failed: %w", err)
	}
	defer func() {
		err = errors.Join(err, store.Close())
	}()

	mains := store.MainArtifacts(ctx)
	if len(mains) != 1 {
		return fmt.Errorf("expected exactly one main artifact in OCI layout, got %d", len(mains))
	}
	if mains[0].Digest != want {
		return fmt.Errorf("OCI artifact digest mismatch: expected %s, got %s", want, mains[0].Digest)
	}
	return ocitar.VerifyGraph(ctx, store, mains[0])
}
