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
	var sourceSpec runtime.Typed
	var v2desc *v2.Descriptor
	switch tr := transformation.(type) {
	case *v1alpha1.OCIAddComponentVersion:
		repoSpec = &tr.Spec.Repository
		v2desc = tr.Spec.Descriptor
		if tr.Spec.SourceRepository != nil {
			sourceSpec = tr.Spec.SourceRepository
		}
	case *v1alpha1.CTFAddComponentVersion:
		repoSpec = &tr.Spec.Repository
		v2desc = tr.Spec.Descriptor
		if tr.Spec.SourceRepository != nil {
			sourceSpec = tr.Spec.SourceRepository
		}
	default:
		return nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}

	var creds runtime.Typed
	if t.CredentialProvider != nil {
		if consumerId, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repoSpec); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil {
				if !errors.Is(err, credentials.ErrNotFound) {
					return nil, fmt.Errorf("failed resolving credentials: %w", err)
				}
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

	if sourceSpec != nil {
		if err := t.copyReferrers(ctx, sourceSpec, repo, desc.Component.Name, desc.Component.Version); err != nil {
			return nil, fmt.Errorf("failed copying referrers for %s:%s: %w",
				desc.Component.Name, desc.Component.Version, err)
		}
	}

	return transformation, nil
}

// copyReferrers copies all OCI referrers of the component version manifest from
// the source repository to the target. It is type-agnostic: attestations and any
// other referrer kind travel the same way. It is a no-op when either repository
// does not expose referrers or the source has none. Transfer preserves the
// component version manifest digest, so copied referrers' subject still matches.
func (t *AddComponentVersion) copyReferrers(ctx context.Context, sourceSpec runtime.Typed, target repository.ComponentVersionRepository, component, version string) error {
	targetReferrers, ok := target.(repository.ComponentVersionReferrerRepository)
	if !ok {
		return nil
	}

	var creds runtime.Typed
	if t.CredentialProvider != nil {
		if consumerID, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, sourceSpec); err == nil {
			if resolved, err := t.CredentialProvider.Resolve(ctx, consumerID); err == nil {
				creds = resolved
			} else if !errors.Is(err, credentials.ErrNotFound) {
				return fmt.Errorf("failed resolving source credentials: %w", err)
			}
		}
	}

	source, err := t.RepoProvider.GetComponentVersionRepository(ctx, sourceSpec, creds)
	if err != nil {
		return fmt.Errorf("failed getting source repository: %w", err)
	}
	sourceReferrers, ok := source.(repository.ComponentVersionReferrerRepository)
	if !ok {
		return nil
	}

	referrers, err := sourceReferrers.GetComponentVersionReferrers(ctx, component, version)
	if err != nil {
		return err
	}
	for _, referrer := range referrers {
		if err := targetReferrers.AddComponentVersionReferrer(ctx, component, version, referrer); err != nil {
			return err
		}
	}
	return nil
}
