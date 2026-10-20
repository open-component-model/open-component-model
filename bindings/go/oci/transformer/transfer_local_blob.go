package transformer

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TransferLocalBlob is a fused transformer that streams a local resource from a component
// version in an OCI or CTF repository into the same component version in a target OCI or
// CTF repository, without buffering it in a file.
type TransferLocalBlob struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

func (t *TransferLocalBlob) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.TransferLocalBlob
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to transfer local blob transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for transfer local blob transformation")
	}
	if transformation.Spec.Repository == nil {
		return nil, fmt.Errorf("target repository is required")
	}
	if transformation.Output == nil {
		transformation.Output = &v1alpha1.TransferLocalBlobOutput{}
	}
	src := transformation.Spec.Source

	target, err := localBlobTarget(ctx, t.RepoProvider, t.CredentialProvider, transformation.Spec.Repository, transformation.Spec.GlobalAccessPolicy)
	if err != nil {
		return nil, err
	}

	content, resource, err := getLocalBlob(ctx, t.RepoProvider, t.CredentialProvider, src)
	if err != nil {
		return nil, err
	}

	updated, err := target.AddLocalResource(ctx, src.Component, src.Version, resource, content)
	if err != nil {
		return nil, fmt.Errorf("failed adding local resource %q to component %s:%s: %w", resource.Name, src.Component, src.Version, err)
	}

	if transformation.Output.Resource, err = descriptor.ConvertToV2Resource(oci.DefaultRepositoryScheme, updated); err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}
	return &transformation, nil
}

// TransferOCIArtifactToLocalBlob is a fused transformer that streams an OCI artifact (or a
// single layer) from its registry into a component version in an OCI or CTF repository as a
// local blob, without buffering it in a file.
type TransferOCIArtifactToLocalBlob struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	Repository         repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *TransferOCIArtifactToLocalBlob) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.TransferOCIArtifactToLocalBlob
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to transfer oci artifact to local blob transformation: %w", err)
	}
	spec := transformation.Spec
	switch {
	case spec == nil:
		return nil, fmt.Errorf("spec is required for transfer oci artifact to local blob transformation")
	case spec.Resource == nil:
		return nil, fmt.Errorf("source resource is required")
	case spec.TargetResource == nil:
		return nil, fmt.Errorf("target resource is required")
	case spec.Repository == nil:
		return nil, fmt.Errorf("target repository is required")
	case spec.Component == "":
		return nil, fmt.Errorf("component name is required")
	case spec.Version == "":
		return nil, fmt.Errorf("component version is required")
	}
	if transformation.Output == nil {
		transformation.Output = &v1alpha1.TransferOCIArtifactToLocalBlobOutput{}
	}
	srcResource := descriptor.ConvertFromV2Resource(spec.Resource)
	targetResource := descriptor.ConvertFromV2Resource(spec.TargetResource)

	target, err := localBlobTarget(ctx, t.RepoProvider, t.CredentialProvider, spec.Repository, spec.GlobalAccessPolicy)
	if err != nil {
		return nil, err
	}

	var creds runtime.Typed
	if t.CredentialProvider != nil {
		if consumerID, err := t.Repository.GetResourceCredentialConsumerIdentity(ctx, srcResource); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerID); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving source credentials: %w", err)
			}
		}
	}
	content, err := t.Repository.DownloadResource(ctx, srcResource, creds)
	if err != nil {
		return nil, fmt.Errorf("failed downloading OCI artifact %v: %w", srcResource.ToIdentity(), err)
	}

	updated, err := target.AddLocalResource(ctx, spec.Component, spec.Version, targetResource, content)
	if err != nil {
		return nil, fmt.Errorf("failed adding local resource %q to component %s:%s: %w", targetResource.Name, spec.Component, spec.Version, err)
	}
	if transformation.Output.Resource, err = descriptor.ConvertToV2Resource(oci.DefaultRepositoryScheme, updated); err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}
	return &transformation, nil
}

// localBlobTarget returns the repository local blobs are added to, with the global access
// policy applied. The policy is always set explicitly (including Never) to avoid leaking
// state from prior transforms on the same cached repository instance.
func localBlobTarget(ctx context.Context, provider repository.ComponentVersionRepositoryProvider, resolver credentials.Resolver, spec runtime.Typed, policy ocirepospecv1.GlobalAccessPolicy) (repository.ComponentVersionRepository, error) {
	target, err := componentVersionRepository(ctx, provider, resolver, spec)
	if err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}
	if ociRepo, ok := target.(*oci.Repository); ok {
		switch policy {
		case ocirepospecv1.GlobalAccessPolicyNever:
			ociRepo.SetGlobalAccessPolicy(oci.GlobalAccessPolicyNever)
		case ocirepospecv1.GlobalAccessPolicyAuto:
			ociRepo.SetGlobalAccessPolicy(oci.GlobalAccessPolicyAuto)
		default:
			return nil, fmt.Errorf("unsupported globalAccessPolicy %q", policy)
		}
	} else if policy != ocirepospecv1.GlobalAccessPolicyNever {
		return nil, fmt.Errorf("globalAccessPolicy is only supported for OCI repositories, got %T", target)
	}
	return target, nil
}

// TransferLocalBlobToOCIArtifact is a fused transformer that streams a local resource
// holding an OCI artifact from a component version in an OCI or CTF repository and
// uploads it as a separate OCI artifact, without buffering it in a file.
type TransferLocalBlobToOCIArtifact struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	Repository         repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *TransferLocalBlobToOCIArtifact) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.TransferLocalBlobToOCIArtifact
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to transfer local blob to oci artifact transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for transfer local blob to oci artifact transformation")
	}
	if transformation.Spec.TargetResource == nil {
		return nil, fmt.Errorf("target resource is required")
	}
	if transformation.Output == nil {
		transformation.Output = &v1alpha1.TransferLocalBlobToOCIArtifactOutput{}
	}
	targetResource := descriptor.ConvertFromV2Resource(transformation.Spec.TargetResource)

	var creds runtime.Typed
	if t.CredentialProvider != nil {
		if consumerID, err := t.Repository.GetResourceCredentialConsumerIdentity(ctx, targetResource); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerID); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving target credentials: %w", err)
			}
		}
	}

	content, _, err := getLocalBlob(ctx, t.RepoProvider, t.CredentialProvider, transformation.Spec.Source)
	if err != nil {
		return nil, err
	}

	updated, err := t.Repository.UploadResource(ctx, targetResource, content, creds)
	if err != nil {
		return nil, fmt.Errorf("failed uploading OCI artifact %v: %w", targetResource.ToIdentity(), err)
	}

	if transformation.Output.Resource, err = descriptor.ConvertToV2Resource(t.Scheme, updated); err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}
	return &transformation, nil
}

// getLocalBlob opens the local resource identified by src. The returned blob reads
// from the source repository on demand.
func getLocalBlob(ctx context.Context, provider repository.ComponentVersionRepositoryProvider, resolver credentials.Resolver, src v1alpha1.LocalBlobSource) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	if src.Repository == nil {
		return nil, nil, fmt.Errorf("source repository is required")
	}
	if src.Component == "" {
		return nil, nil, fmt.Errorf("component name is required")
	}
	if src.Version == "" {
		return nil, nil, fmt.Errorf("component version is required")
	}
	if len(src.ResourceIdentity) == 0 {
		return nil, nil, fmt.Errorf("resource identity is required")
	}
	repo, err := componentVersionRepository(ctx, provider, resolver, src.Repository)
	if err != nil {
		return nil, nil, fmt.Errorf("source: %w", err)
	}
	if repo, err = repository.PinRepository(repo, src.Component, src.Version, src.Digest); err != nil {
		return nil, nil, err
	}
	content, resource, err := repo.GetLocalResource(ctx, src.Component, src.Version, src.ResourceIdentity)
	if err != nil {
		return nil, nil, fmt.Errorf("failed getting local resource %v from component %s:%s: %w", src.ResourceIdentity, src.Component, src.Version, err)
	}
	return content, resource, nil
}

func componentVersionRepository(ctx context.Context, provider repository.ComponentVersionRepositoryProvider, resolver credentials.Resolver, spec runtime.Typed) (repository.ComponentVersionRepository, error) {
	var creds runtime.Typed
	if resolver != nil {
		if consumerID, err := provider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, spec); err == nil {
			if creds, err = resolver.Resolve(ctx, consumerID); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving credentials: %w", err)
			}
		}
	}
	repo, err := provider.GetComponentVersionRepository(ctx, spec, creds)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %w", err)
	}
	return repo, nil
}
