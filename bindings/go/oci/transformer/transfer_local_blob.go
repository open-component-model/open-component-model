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
// CTF repository, without buffering it in a file. It handles the
// Transfer{OCI,CTF}LocalBlobTo{OCI,CTF}LocalBlob transformations.
type TransferLocalBlob struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

func (t *TransferLocalBlob) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating transfer local blob transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to transfer local blob transformation: %w", err)
	}

	var (
		src    localBlobSource
		dst    localBlobTarget
		output **v1alpha1.TransferLocalBlobOutput
	)
	switch tr := transformation.(type) {
	case *v1alpha1.TransferOCILocalBlobToOCILocalBlob:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		src = ociLocalBlobSource(&tr.Spec.Source)
		dst = localBlobTarget{repository: &tr.Spec.Repository, globalAccessPolicy: tr.Spec.GlobalAccessPolicy}
		output = &tr.Output
	case *v1alpha1.TransferOCILocalBlobToCTFLocalBlob:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		src = ociLocalBlobSource(&tr.Spec.Source)
		dst = localBlobTarget{repository: &tr.Spec.Repository}
		output = &tr.Output
	case *v1alpha1.TransferCTFLocalBlobToOCILocalBlob:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		src = ctfLocalBlobSource(&tr.Spec.Source)
		dst = localBlobTarget{repository: &tr.Spec.Repository, globalAccessPolicy: tr.Spec.GlobalAccessPolicy}
		output = &tr.Output
	case *v1alpha1.TransferCTFLocalBlobToCTFLocalBlob:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		src = ctfLocalBlobSource(&tr.Spec.Source)
		dst = localBlobTarget{repository: &tr.Spec.Repository}
		output = &tr.Output
	default:
		return nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}

	target, err := dst.open(ctx, t.RepoProvider, t.CredentialProvider)
	if err != nil {
		return nil, err
	}

	content, resource, err := src.open(ctx, t.RepoProvider, t.CredentialProvider)
	if err != nil {
		return nil, err
	}

	updated, err := target.AddLocalResource(ctx, src.component, src.version, resource, content)
	if err != nil {
		return nil, fmt.Errorf("failed adding local resource %q to component %s:%s: %w", resource.Name, src.component, src.version, err)
	}

	v2Resource, err := descriptor.ConvertToV2Resource(oci.DefaultRepositoryScheme, updated)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}
	*output = &v1alpha1.TransferLocalBlobOutput{Resource: v2Resource}
	return transformation, nil
}

// TransferOCIArtifactToLocalBlob is a fused transformer that streams an OCI artifact (or a
// single layer) from its registry into a component version in an OCI or CTF repository as a
// local blob, without buffering it in a file. It handles the
// TransferOCIArtifactTo{OCI,CTF}LocalBlob transformations.
type TransferOCIArtifactToLocalBlob struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	Repository         repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *TransferOCIArtifactToLocalBlob) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating transfer oci artifact to local blob transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to transfer oci artifact to local blob transformation: %w", err)
	}

	var (
		dst                            localBlobTarget
		component, version             string
		sourceResource, targetResource *descriptor.Resource
		output                         **v1alpha1.TransferLocalBlobOutput
	)
	switch tr := transformation.(type) {
	case *v1alpha1.TransferOCIArtifactToOCILocalBlob:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		dst = localBlobTarget{repository: &tr.Spec.Repository, globalAccessPolicy: tr.Spec.GlobalAccessPolicy}
		component, version = tr.Spec.Component, tr.Spec.Version
		sourceResource, targetResource = descriptor.ConvertFromV2Resource(tr.Spec.Resource), descriptor.ConvertFromV2Resource(tr.Spec.TargetResource)
		output = &tr.Output
	case *v1alpha1.TransferOCIArtifactToCTFLocalBlob:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		dst = localBlobTarget{repository: &tr.Spec.Repository}
		component, version = tr.Spec.Component, tr.Spec.Version
		sourceResource, targetResource = descriptor.ConvertFromV2Resource(tr.Spec.Resource), descriptor.ConvertFromV2Resource(tr.Spec.TargetResource)
		output = &tr.Output
	default:
		return nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}
	switch {
	case sourceResource == nil:
		return nil, fmt.Errorf("source resource is required")
	case targetResource == nil:
		return nil, fmt.Errorf("target resource is required")
	case component == "":
		return nil, fmt.Errorf("component name is required")
	case version == "":
		return nil, fmt.Errorf("component version is required")
	}

	target, err := dst.open(ctx, t.RepoProvider, t.CredentialProvider)
	if err != nil {
		return nil, err
	}

	var creds runtime.Typed
	if t.CredentialProvider != nil {
		if consumerID, err := t.Repository.GetResourceCredentialConsumerIdentity(ctx, sourceResource); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerID); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving source credentials: %w", err)
			}
		}
	}
	content, err := t.Repository.DownloadResource(ctx, sourceResource, creds)
	if err != nil {
		return nil, fmt.Errorf("failed downloading OCI artifact %v: %w", sourceResource.ToIdentity(), err)
	}

	updated, err := target.AddLocalResource(ctx, component, version, targetResource, content)
	if err != nil {
		return nil, fmt.Errorf("failed adding local resource %q to component %s:%s: %w", targetResource.Name, component, version, err)
	}
	v2Resource, err := descriptor.ConvertToV2Resource(oci.DefaultRepositoryScheme, updated)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}
	*output = &v1alpha1.TransferLocalBlobOutput{Resource: v2Resource}
	return transformation, nil
}

// TransferLocalBlobToOCIArtifact is a fused transformer that streams a local resource
// holding an OCI artifact from a component version in an OCI or CTF repository and
// uploads it as a separate OCI artifact, without buffering it in a file. It handles the
// Transfer{OCI,CTF}LocalBlobToOCIArtifact transformations.
type TransferLocalBlobToOCIArtifact struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	Repository         repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *TransferLocalBlobToOCIArtifact) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating transfer local blob to oci artifact transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to transfer local blob to oci artifact transformation: %w", err)
	}

	var (
		src            localBlobSource
		targetResource *descriptor.Resource
		output         **v1alpha1.TransferLocalBlobToOCIArtifactOutput
	)
	switch tr := transformation.(type) {
	case *v1alpha1.TransferOCILocalBlobToOCIArtifact:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		src = ociLocalBlobSource(&tr.Spec.Source)
		targetResource = descriptor.ConvertFromV2Resource(tr.Spec.TargetResource)
		output = &tr.Output
	case *v1alpha1.TransferCTFLocalBlobToOCIArtifact:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		src = ctfLocalBlobSource(&tr.Spec.Source)
		targetResource = descriptor.ConvertFromV2Resource(tr.Spec.TargetResource)
		output = &tr.Output
	default:
		return nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}
	if targetResource == nil {
		return nil, fmt.Errorf("target resource is required")
	}

	var creds runtime.Typed
	if t.CredentialProvider != nil {
		if consumerID, err := t.Repository.GetResourceCredentialConsumerIdentity(ctx, targetResource); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerID); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving target credentials: %w", err)
			}
		}
	}

	content, _, err := src.open(ctx, t.RepoProvider, t.CredentialProvider)
	if err != nil {
		return nil, err
	}

	updated, err := t.Repository.UploadResource(ctx, targetResource, content, creds)
	if err != nil {
		return nil, fmt.Errorf("failed uploading OCI artifact %v: %w", targetResource.ToIdentity(), err)
	}

	v2Resource, err := descriptor.ConvertToV2Resource(t.Scheme, updated)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}
	*output = &v1alpha1.TransferLocalBlobToOCIArtifactOutput{Resource: v2Resource}
	return transformation, nil
}

// localBlobSource is the repository-kind independent view of an
// v1alpha1.OCILocalBlobSource or v1alpha1.CTFLocalBlobSource.
type localBlobSource struct {
	repository                 runtime.Typed
	component, version, digest string
	resourceIdentity           runtime.Identity
}

func ociLocalBlobSource(src *v1alpha1.OCILocalBlobSource) localBlobSource {
	return localBlobSource{
		repository:       &src.Repository,
		component:        src.Component,
		version:          src.Version,
		digest:           src.Digest,
		resourceIdentity: src.ResourceIdentity,
	}
}

func ctfLocalBlobSource(src *v1alpha1.CTFLocalBlobSource) localBlobSource {
	return localBlobSource{
		repository:       &src.Repository,
		component:        src.Component,
		version:          src.Version,
		digest:           src.Digest,
		resourceIdentity: src.ResourceIdentity,
	}
}

// open opens the local resource identified by src. The returned blob reads
// from the source repository on demand.
func (src localBlobSource) open(ctx context.Context, provider repository.ComponentVersionRepositoryProvider, resolver credentials.Resolver) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	if src.component == "" {
		return nil, nil, fmt.Errorf("component name is required")
	}
	if src.version == "" {
		return nil, nil, fmt.Errorf("component version is required")
	}
	if len(src.resourceIdentity) == 0 {
		return nil, nil, fmt.Errorf("resource identity is required")
	}
	repo, err := componentVersionRepository(ctx, provider, resolver, src.repository)
	if err != nil {
		return nil, nil, fmt.Errorf("source: %w", err)
	}
	if repo, err = repository.PinRepository(repo, src.component, src.version, src.digest); err != nil {
		return nil, nil, err
	}
	content, resource, err := repo.GetLocalResource(ctx, src.component, src.version, src.resourceIdentity)
	if err != nil {
		return nil, nil, fmt.Errorf("failed getting local resource %v from component %s:%s: %w", src.resourceIdentity, src.component, src.version, err)
	}
	return content, resource, nil
}

// localBlobTarget is the repository local blobs are added to. globalAccessPolicy is only
// set by the transformations targeting an OCI repository.
type localBlobTarget struct {
	repository         runtime.Typed
	globalAccessPolicy ocirepospecv1.GlobalAccessPolicy
}

// open returns the target repository. For an OCI repository the global access policy is
// always set explicitly (including Never) to avoid leaking state from prior transforms on
// the same cached repository instance.
func (dst localBlobTarget) open(ctx context.Context, provider repository.ComponentVersionRepositoryProvider, resolver credentials.Resolver) (repository.ComponentVersionRepository, error) {
	target, err := componentVersionRepository(ctx, provider, resolver, dst.repository)
	if err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}
	if _, ok := dst.repository.(*ocirepospecv1.Repository); !ok {
		return target, nil
	}
	if ociRepo, ok := target.(*oci.Repository); ok {
		switch dst.globalAccessPolicy {
		case ocirepospecv1.GlobalAccessPolicyNever:
			ociRepo.SetGlobalAccessPolicy(oci.GlobalAccessPolicyNever)
		case ocirepospecv1.GlobalAccessPolicyAuto:
			ociRepo.SetGlobalAccessPolicy(oci.GlobalAccessPolicyAuto)
		default:
			return nil, fmt.Errorf("unsupported globalAccessPolicy %q", dst.globalAccessPolicy)
		}
	} else if dst.globalAccessPolicy != ocirepospecv1.GlobalAccessPolicyNever {
		return nil, fmt.Errorf("globalAccessPolicy is only supported for OCI repositories, got %T", target)
	}
	return target, nil
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
