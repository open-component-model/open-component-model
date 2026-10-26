package transformer

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CarryComponentSignatures copies the signature referrers of a normalized-layout
// component version from the source repository into the target repository.
// It runs after the component version was added to the target. Any failure
// fails the transformation, so a transfer never silently drops signatures.
type CarryComponentSignatures struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

func (t *CarryComponentSignatures) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating carry component signatures transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to carry component signatures transformation: %w", err)
	}

	var targetSpec runtime.Typed
	var sourceSpec *runtime.Raw
	var component, version string
	switch tr := transformation.(type) {
	case *v1alpha1.OCICarryComponentSignatures:
		targetSpec = &tr.Spec.Repository
		sourceSpec = tr.Spec.SourceRepository
		component, version = tr.Spec.Component, tr.Spec.Version
	case *v1alpha1.CTFCarryComponentSignatures:
		targetSpec = &tr.Spec.Repository
		sourceSpec = tr.Spec.SourceRepository
		component, version = tr.Spec.Component, tr.Spec.Version
	default:
		return nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}
	if sourceSpec == nil {
		return nil, fmt.Errorf("source repository is required to carry signatures of %s:%s", component, version)
	}

	target, err := openComponentVersionRepository(ctx, t.RepoProvider, t.CredentialProvider, targetSpec)
	if err != nil {
		return nil, fmt.Errorf("target repository: %w", err)
	}
	carrier, ok := target.(repository.ComponentSignatureCarrier)
	if !ok {
		return nil, fmt.Errorf("target repository %T cannot carry component signatures", target)
	}

	source, err := openComponentVersionRepository(ctx, t.RepoProvider, t.CredentialProvider, sourceSpec)
	if err != nil {
		return nil, fmt.Errorf("source repository: %w", err)
	}

	if err := carrier.CarryComponentSignatures(ctx, source, component, version); err != nil {
		return nil, fmt.Errorf("failed carrying signatures of component version %s:%s: %w", component, version, err)
	}

	return transformation, nil
}
