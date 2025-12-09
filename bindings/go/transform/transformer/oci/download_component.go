package oci

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	v2runtime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/transformations/ctf"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/transformations/oci"
)

type DownloadComponent struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

func (t *DownloadComponent) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
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
	case *oci.DownloadComponentTransformation:
		repoSpec = &tr.Spec.Repository
		component, version = tr.Spec.Component, tr.Spec.Version
	case *ctf.DownloadComponentTransformation:
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
	// TODO(fabianburth): throw an error if one attempts to marshal a runtime
	//  descriptor
	desc, err := repo.GetComponentVersion(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version %s:%s: %v", component, version, err)
	}

	v2desc, err := v2runtime.ConvertToV2(runtime.NewScheme(), desc)
	if err != nil {
		return nil, fmt.Errorf("failed converting component version to v2: %v", err)
	}

	switch transformation := transformation.(type) {
	case *ctf.DownloadComponentTransformation:
		transformation.Output = &ctf.DownloadComponentTransformationOutput{
			Descriptor: v2desc,
		}
	case *oci.DownloadComponentTransformation:
		transformation.Output = &oci.DownloadComponentTransformationOutput{
			Descriptor: v2desc,
		}
	}
	return transformation, err
}
