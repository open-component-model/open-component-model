package transformer

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type AddComponentVersion struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

func (t *AddComponentVersion) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating download component transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to download component transformation: %w", err)
	}
	var repoSpec runtime.Typed
	var v2desc *v2.Descriptor
	switch tr := transformation.(type) {
	case *v1alpha1.OCIAddComponentVersion:
		repoSpec = &tr.Spec.Repository
		v2desc = tr.Spec.Descriptor
	case *v1alpha1.CTFAddComponentVersion:
		repoSpec = &tr.Spec.Repository
		v2desc = tr.Spec.Descriptor
	default:
		return nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}

	repo, err := openComponentVersionRepository(ctx, t.RepoProvider, t.CredentialProvider, repoSpec)
	if err != nil {
		return nil, err
	}

	desc, err := descriptor.ConvertFromV2(v2desc)
	if err != nil {
		return nil, fmt.Errorf("failed converting component version from v2: %w", err)
	}

	if err := repo.AddComponentVersion(ctx, desc); err != nil {
		return nil, fmt.Errorf("failed getting component version %s:%s: %w",
			desc.Component.Name, desc.Component.Version, err)
	}

	return transformation, nil
}

// openComponentVersionRepository opens the repository for spec, resolving its
// credentials if a credential provider is configured. Missing credentials are
// not an error: the repository may allow anonymous access.
func openComponentVersionRepository(
	ctx context.Context,
	provider repository.ComponentVersionRepositoryProvider,
	credentialProvider credentials.Resolver,
	spec runtime.Typed,
) (repository.ComponentVersionRepository, error) {
	var creds runtime.Typed
	if credentialProvider != nil {
		if consumerId, err := provider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, spec); err == nil {
			if creds, err = credentialProvider.Resolve(ctx, consumerId); err != nil {
				if !errors.Is(err, credentials.ErrNotFound) {
					return nil, fmt.Errorf("failed resolving credentials: %w", err)
				}
			}
		}
	}

	repo, err := provider.GetComponentVersionRepository(ctx, spec, creds)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %w", err)
	}
	return repo, nil
}
