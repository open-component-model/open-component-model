package transformer

import (
	"context"
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type GetComponentVersion struct {
	Scheme       *runtime.Scheme
	RepoProvider repository.ComponentVersionRepositoryProvider
}

func (t *GetComponentVersion) GetCredentialConsumerIdentities(ctx context.Context, step runtime.Typed) (map[string]runtime.Identity, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating download component transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to download component transformation: %w", err)
	}
	var repoSpec runtime.Typed
	switch tr := transformation.(type) {
	case *v1alpha1.OCIGetComponentVersion:
		repoSpec = &tr.Spec.Repository
	case *v1alpha1.CTFGetComponentVersion:
		repoSpec = &tr.Spec.Repository
	default:
		return nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}

	identity, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repoSpec)
	if err != nil {
		return nil, err
	}
	if identity == nil {
		return nil, nil
	}
	return map[string]runtime.Identity{"repository": identity}, nil
}

func (t *GetComponentVersion) Transform(ctx context.Context, step runtime.Typed, credentials map[string]map[string]string) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating download component transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to download component transformation: %w", err)
	}
	var repoSpec runtime.Typed
	var component, version string
	switch tr := transformation.(type) {
	case *v1alpha1.OCIGetComponentVersion:
		repoSpec = &tr.Spec.Repository
		component, version = tr.Spec.Component, tr.Spec.Version
	case *v1alpha1.CTFGetComponentVersion:
		repoSpec = &tr.Spec.Repository
		component, version = tr.Spec.Component, tr.Spec.Version
	default:
		return nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}

	var creds map[string]string
	if credentials != nil {
		creds = credentials["repository"]
	}

	repo, err := t.RepoProvider.GetComponentVersionRepository(ctx, repoSpec, creds)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %w", err)
	}

	desc, err := repo.GetComponentVersion(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version %s:%s: %w", component, version, err)
	}

	// TODO(jakobmoellerdev): mabye use the OCI scheme here instead of a blank scheme.
	v2desc, err := descriptor.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
	if err != nil {
		return nil, fmt.Errorf("failed converting component version to v2: %w", err)
	}

	switch transformation := transformation.(type) {
	case *v1alpha1.CTFGetComponentVersion:
		transformation.Output = &v1alpha1.CTFGetComponentVersionOutput{
			Descriptor: v2desc,
		}
	case *v1alpha1.OCIGetComponentVersion:
		transformation.Output = &v1alpha1.OCIGetComponentVersionOutput{
			Descriptor: v2desc,
		}
	}
	return transformation, err
}
