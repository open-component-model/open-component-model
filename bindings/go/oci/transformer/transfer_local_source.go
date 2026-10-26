package transformer

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TransferLocalSource is a fused transformer that streams a local source from a component
// version in an OCI or CTF repository into the same component version in a target OCI or
// CTF repository, without buffering it in a file. It handles the
// Transfer{OCI,CTF}LocalSourceTo{OCI,CTF}LocalSource transformations.
type TransferLocalSource struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

func (t *TransferLocalSource) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating transfer local source transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to transfer local source transformation: %w", err)
	}

	var (
		src    localSource
		dst    localBlobTarget
		output **v1alpha1.TransferLocalSourceOutput
	)
	switch tr := transformation.(type) {
	case *v1alpha1.TransferOCILocalSourceToOCILocalSource:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		src = ociLocalSource(&tr.Spec.Source)
		dst = localBlobTarget{repository: &tr.Spec.Repository, globalAccessPolicy: tr.Spec.GlobalAccessPolicy}
		output = &tr.Output
	case *v1alpha1.TransferOCILocalSourceToCTFLocalSource:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		src = ociLocalSource(&tr.Spec.Source)
		dst = localBlobTarget{repository: &tr.Spec.Repository}
		output = &tr.Output
	case *v1alpha1.TransferCTFLocalSourceToOCILocalSource:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		src = ctfLocalSource(&tr.Spec.Source)
		dst = localBlobTarget{repository: &tr.Spec.Repository, globalAccessPolicy: tr.Spec.GlobalAccessPolicy}
		output = &tr.Output
	case *v1alpha1.TransferCTFLocalSourceToCTFLocalSource:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		src = ctfLocalSource(&tr.Spec.Source)
		dst = localBlobTarget{repository: &tr.Spec.Repository}
		output = &tr.Output
	default:
		return nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}

	target, err := dst.open(ctx, t.RepoProvider, t.CredentialProvider)
	if err != nil {
		return nil, err
	}

	content, source, err := src.open(ctx, t.RepoProvider, t.CredentialProvider)
	if err != nil {
		return nil, err
	}
	defer releaseBlob(ctx, content)

	updated, err := target.AddLocalSource(ctx, src.component, src.version, source, content)
	if err != nil {
		return nil, fmt.Errorf("failed adding local source %q to component %s:%s: %w", source.Name, src.component, src.version, err)
	}

	v2Sources, err := descriptor.ConvertToV2Sources(oci.DefaultRepositoryScheme, []descriptor.Source{*updated})
	if err != nil {
		return nil, fmt.Errorf("failed converting source to v2 format: %w", err)
	}
	*output = &v1alpha1.TransferLocalSourceOutput{Source: &v2Sources[0]}
	return transformation, nil
}

// localSource is the repository-kind independent view of an
// v1alpha1.OCILocalSource or v1alpha1.CTFLocalSource.
type localSource struct {
	repository                 runtime.Typed
	component, version, digest string
	sourceIdentity             runtime.Identity
}

func ociLocalSource(src *v1alpha1.OCILocalSource) localSource {
	return localSource{
		repository:     &src.Repository,
		component:      src.Component,
		version:        src.Version,
		digest:         src.Digest,
		sourceIdentity: src.SourceIdentity,
	}
}

func ctfLocalSource(src *v1alpha1.CTFLocalSource) localSource {
	return localSource{
		repository:     &src.Repository,
		component:      src.Component,
		version:        src.Version,
		digest:         src.Digest,
		sourceIdentity: src.SourceIdentity,
	}
}

// open opens the local source identified by src. The returned blob reads
// from the source repository on demand.
func (src localSource) open(ctx context.Context, provider repository.ComponentVersionRepositoryProvider, resolver credentials.Resolver) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	if src.component == "" {
		return nil, nil, fmt.Errorf("component name is required")
	}
	if src.version == "" {
		return nil, nil, fmt.Errorf("component version is required")
	}
	if len(src.sourceIdentity) == 0 {
		return nil, nil, fmt.Errorf("source identity is required")
	}
	repo, err := componentVersionRepository(ctx, provider, resolver, src.repository)
	if err != nil {
		return nil, nil, fmt.Errorf("source: %w", err)
	}
	if repo, err = repository.PinRepository(repo, src.component, src.version, src.digest); err != nil {
		return nil, nil, err
	}
	content, source, err := repo.GetLocalSource(ctx, src.component, src.version, src.sourceIdentity)
	if err != nil {
		return nil, nil, fmt.Errorf("failed getting local source %v from component %s:%s: %w", src.sourceIdentity, src.component, src.version, err)
	}
	return content, source, nil
}
