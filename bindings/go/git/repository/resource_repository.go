package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	gitinternal "ocm.software/open-component-model/bindings/go/git/internal"
	"ocm.software/open-component-model/bindings/go/git/internal/download"
	"ocm.software/open-component-model/bindings/go/git/spec/access"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/git/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type ResourceRepository struct {
	options download.Options
}

var (
	_ repository.ResourceRepository      = (*ResourceRepository)(nil)
	_ repository.ResourceDigestProcessor = (*ResourceRepository)(nil)
)

func NewResourceRepository(opts ...Option) *ResourceRepository {
	r := &ResourceRepository{options: download.Options{MaxDownloadSize: download.DefaultMaxDownloadSize}}
	for _, opt := range opts {
		opt(r)
	}

	return r
}

func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return access.Scheme
}

func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(_ context.Context, res *descriptor.Resource) (runtime.Identity, error) {
	if res == nil {
		return nil, fmt.Errorf("resource is required")
	}

	spec, err := gitinternal.AccessFrom(res.Access)
	if err != nil {
		return nil, err
	}

	return identityv1.IdentityFromURL(spec.Repository)
}

func (r *ResourceRepository) DownloadResource(ctx context.Context, res *descriptor.Resource, creds runtime.Typed) (blob.ReadOnlyBlob, error) {
	b, _, err := r.download(ctx, res, creds)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (r *ResourceRepository) download(ctx context.Context, res *descriptor.Resource, creds runtime.Typed) (*download.Blob, string, error) {
	if res == nil {
		return nil, "", fmt.Errorf("resource is required")
	}

	spec, err := gitinternal.AccessFrom(res.Access)
	if err != nil {
		return nil, "", err
	}

	typed, err := credsv1.ConvertToGitCredentials(creds)
	if err != nil {
		return nil, "", err
	}

	b, commit, err := download.Download(ctx, spec, typed, r.options)
	if err != nil {
		return nil, "", err
	}

	raw, _ := b.Digest()
	if err := gitinternal.VerifyDigest(res.Digest, strings.TrimPrefix(raw, "sha256:")); err != nil {
		return nil, "", errors.Join(err, b.Close())
	}

	return b, commit, nil
}

func (r *ResourceRepository) UploadResource(context.Context, *descriptor.Resource, blob.ReadOnlyBlob, runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("git repositories do not support upload operations")
}

func (r *ResourceRepository) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, res *descriptor.Resource) (runtime.Identity, error) {
	return r.GetResourceCredentialConsumerIdentity(ctx, res)
}

// ProcessResourceDigest pins the access and hashes the same snapshot in one download.
func (r *ResourceRepository) ProcessResourceDigest(ctx context.Context, res *descriptor.Resource, creds runtime.Typed) (_ *descriptor.Resource, err error) {
	b, commit, err := r.download(ctx, res, creds)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, b.Close()) }()
	result := res.DeepCopy()
	spec, err := gitinternal.AccessFrom(result.Access)
	if err != nil {
		return nil, err
	}

	spec.Commit = commit
	pinned := &runtime.Raw{}
	if err := access.Scheme.Convert(spec, pinned); err != nil {
		return nil, fmt.Errorf("cannot encode pinned git access: %w", err)
	}

	result.Access = pinned
	raw, _ := b.Digest()
	result.Digest = &descriptor.Digest{
		HashAlgorithm:          gitinternal.HashAlgorithm,
		NormalisationAlgorithm: gitinternal.NormalisationAlgorithm,
		Value:                  strings.TrimPrefix(raw, "sha256:"),
	}

	return result, nil
}
