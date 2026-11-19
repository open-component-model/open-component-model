package repository

import (
	"context"
	"errors"
	"fmt"
	"io"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository/integrity"
)

// GetLocalResource reads res, a local resource of component in version, through repo and
// binds it to the digest res records.
//
// repo verifies the content against the resource it returns (see
// [ComponentVersionRepository.GetLocalResource]). That resource is read again by version,
// so it must record the digest res records; otherwise a version tag that moved since res
// was resolved (or pinned) could substitute content.
func GetLocalResource(ctx context.Context, repo ComponentVersionRepository, component, version string, res *descriptor.Resource) (blob.ReadOnlyBlob, error) {
	data, got, err := repo.GetLocalResource(ctx, component, version, res.ToIdentity())
	if err != nil {
		return nil, err
	}
	var gotDigest *descriptor.Digest
	if got != nil {
		gotDigest = got.Digest
	}
	if !sameDigest(gotDigest, res.Digest) {
		return nil, fmt.Errorf("local resource %q of %s:%s records digest %s, but the resolved component version records %s",
			res.Name, component, version, formatDigest(gotDigest), formatDigest(res.Digest))
	}
	return data, nil
}

// VerifyLocalResource checks that the content of res, a local resource of component in
// version, matches the digest res records, without using the content. The content is
// only downloaded if repo cannot check it otherwise: a blob is matched against the digest
// the repository stores it under, and an OCI artifact against its resolved manifest, but
// content that is only checked while it is read (see [integrity.VerifiedOnRead]) is read
// to its end.
func VerifyLocalResource(ctx context.Context, repo ComponentVersionRepository, component, version string, res *descriptor.Resource) error {
	data, err := GetLocalResource(ctx, repo, component, version, res)
	if err != nil {
		return err
	}
	if !integrity.VerifiedOnRead(data) {
		return nil
	}
	rc, err := data.ReadCloser()
	if err != nil {
		return err
	}
	_, err = blob.CopyReader(io.Discard, rc)
	return errors.Join(err, rc.Close())
}

func sameDigest(a, b *descriptor.Digest) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func formatDigest(d *descriptor.Digest) string {
	if d == nil {
		return "<none>"
	}
	return fmt.Sprintf("%s:%s (%s)", d.HashAlgorithm, d.Value, d.NormalisationAlgorithm)
}
