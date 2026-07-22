package transformer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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
	var sourceRepoSpec runtime.Typed
	var v2desc *v2.Descriptor
	switch tr := transformation.(type) {
	case *v1alpha1.OCIAddComponentVersion:
		repoSpec = &tr.Spec.Repository
		v2desc = tr.Spec.Descriptor
		if tr.Spec.SourceRepository != nil {
			sourceRepoSpec = tr.Spec.SourceRepository
		}
	case *v1alpha1.CTFAddComponentVersion:
		repoSpec = &tr.Spec.Repository
		v2desc = tr.Spec.Descriptor
		if tr.Spec.SourceRepository != nil {
			sourceRepoSpec = tr.Spec.SourceRepository
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

	// If a source repository was threaded into the step, attempt to carry
	// component-level signature referrers (e.g. cosign signatures) from the
	// source into the target. This is an OPTIONAL enhancement: the component
	// version has already been transferred successfully above, so a failure to
	// carry signatures must NOT fail the transfer. It is a genuine no-op for
	// targets that do not implement signature carrying, for non-OCI sources,
	// and whenever source and target do not resolve the same component-manifest
	// digest (only the normalized layout preserves it).
	if sourceRepoSpec != nil {
		if err := t.carrySignatures(ctx, repo, sourceRepoSpec, desc); err != nil {
			slog.WarnContext(ctx, "component transferred, but carrying signature referrers failed",
				slog.String("component", desc.Component.Name),
				slog.String("version", desc.Component.Version),
				slog.Any("error", err))
		}
	}

	return transformation, nil
}

// carrySignatures copies component-manifest signature referrers from the source repository (built
// from sourceRepoSpec) into the target repo, when the target supports it. Returns an error so the
// caller can log it; callers MUST treat the error as non-fatal (the transfer already succeeded).
func (t *AddComponentVersion) carrySignatures(ctx context.Context, target repository.ComponentVersionRepository, sourceRepoSpec runtime.Typed, desc *descriptor.Descriptor) error {
	carrier, ok := target.(repository.ComponentSignatureCarrier)
	if !ok {
		return nil // target cannot carry signatures; nothing to do
	}
	var srcCreds runtime.Typed
	if t.CredentialProvider != nil {
		if consumerId, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, sourceRepoSpec); err == nil {
			if srcCreds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil {
				if !errors.Is(err, credentials.ErrNotFound) {
					return fmt.Errorf("resolve source credentials for signature carry: %w", err)
				}
			}
		}
	}
	srcRepo, err := t.RepoProvider.GetComponentVersionRepository(ctx, sourceRepoSpec, srcCreds)
	if err != nil {
		return fmt.Errorf("open source repository for signature carry: %w", err)
	}
	if err := carrier.CarryComponentSignatures(ctx, srcRepo, desc.Component.Name, desc.Component.Version); err != nil {
		return fmt.Errorf("carry component signatures: %w", err)
	}
	return nil
}
