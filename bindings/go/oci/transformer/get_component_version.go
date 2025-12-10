package transformer

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type GetComponentVersion struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

func (t *GetComponentVersion) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating download component transformation object: %v", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to download component transformation: %v", err)
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
	if t.CredentialProvider != nil {
		if consumerId, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repoSpec); err == nil {
			creds, err = t.CredentialProvider.Resolve(ctx, consumerId)
			if err != nil {
				return nil, fmt.Errorf("failed resolving credentials: %v", err)
			}
		}
	}

	repo, err := t.RepoProvider.GetComponentVersionRepository(ctx, repoSpec, creds)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %v", err)
	}

	desc, err := repo.GetComponentVersion(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version %s:%s: %v", component, version, err)
	}

	// TODO(jakobmoellerdev): mabye use the OCI scheme here instead of a blank scheme.
	v2desc, err := descriptor.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
	if err != nil {
		return nil, fmt.Errorf("failed converting component version to v2: %v", err)
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
