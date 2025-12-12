package transformer

import (
	"context"
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

	var creds map[string]string
	if t.CredentialProvider != nil {
		if consumerId, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repoSpec); err == nil {
			creds, err = t.CredentialProvider.Resolve(ctx, consumerId)
			if err != nil {
				return nil, fmt.Errorf("failed resolving credentials: %w", err)
			}
		}
	}

	repo, err := t.RepoProvider.GetComponentVersionRepository(ctx, repoSpec, creds)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %w", err)
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
