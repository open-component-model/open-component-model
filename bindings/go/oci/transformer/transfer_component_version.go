package transformer

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TransferComponentVersion is a fused transformer that adds a component version stored in the
// normalized layout in an OCI or CTF repository to a target OCI or CTF repository. The normalized
// manifest and its signature referrers are copied from the source store into the target store,
// without an intermediate file, so signatures over the manifest digest stay valid. It handles the
// Transfer{OCI,CTF}ComponentVersionTo{OCI,CTF} transformations.
//
// Every transformation reads the source graph itself: a graph is not shared between the
// transformations of several targets.
type TransferComponentVersion struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

// componentGraphSource is a repository that hands out the OCI graph a normalized component
// version is stored as.
type componentGraphSource interface {
	DownloadComponentVersionGraph(ctx context.Context, component, version string) (ocistream.ResourceStream, error)
}

// componentGraphTarget is a repository that adds a component version from such a graph.
type componentGraphTarget interface {
	AddComponentVersionFromGraph(ctx context.Context, desc *descriptor.Descriptor, graph ocistream.ResourceStream) error
}

func (t *TransferComponentVersion) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating transfer component version transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to transfer component version transformation: %w", err)
	}

	var (
		src        componentVersionSource
		targetSpec runtime.Typed
		v2desc     *v2.Descriptor
		output     **v1alpha1.TransferComponentVersionOutput
	)
	switch tr := transformation.(type) {
	case *v1alpha1.TransferOCIComponentVersionToOCI:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		src = ociComponentVersionSource(&tr.Spec.Source)
		targetSpec, v2desc, output = &tr.Spec.Repository, tr.Spec.Descriptor, &tr.Output
	case *v1alpha1.TransferOCIComponentVersionToCTF:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		src = ociComponentVersionSource(&tr.Spec.Source)
		targetSpec, v2desc, output = &tr.Spec.Repository, tr.Spec.Descriptor, &tr.Output
	case *v1alpha1.TransferCTFComponentVersionToOCI:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		src = ctfComponentVersionSource(&tr.Spec.Source)
		targetSpec, v2desc, output = &tr.Spec.Repository, tr.Spec.Descriptor, &tr.Output
	case *v1alpha1.TransferCTFComponentVersionToCTF:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		src = ctfComponentVersionSource(&tr.Spec.Source)
		targetSpec, v2desc, output = &tr.Spec.Repository, tr.Spec.Descriptor, &tr.Output
	default:
		return nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}
	if v2desc == nil {
		return nil, fmt.Errorf("descriptor is required")
	}

	desc, err := descriptor.ConvertFromV2(v2desc)
	if err != nil {
		return nil, fmt.Errorf("failed converting component version from v2: %w", err)
	}

	targetRepo, err := componentVersionRepository(ctx, t.RepoProvider, t.CredentialProvider, targetSpec)
	if err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}
	target, ok := targetRepo.(componentGraphTarget)
	if !ok {
		return nil, fmt.Errorf("target repository %T cannot add a component version from its OCI graph", targetRepo)
	}

	graph, err := src.open(ctx, t.RepoProvider, t.CredentialProvider)
	if err != nil {
		return nil, err
	}

	if err := target.AddComponentVersionFromGraph(ctx, desc, graph); err != nil {
		return nil, fmt.Errorf("failed adding component version %s:%s: %w", desc.Component.Name, desc.Component.Version, err)
	}

	*output = &v1alpha1.TransferComponentVersionOutput{}
	return transformation, nil
}

// componentVersionSource is the repository-kind independent view of an
// v1alpha1.OCIComponentVersionSource or v1alpha1.CTFComponentVersionSource.
type componentVersionSource struct {
	repository                 runtime.Typed
	component, version, digest string
}

func ociComponentVersionSource(src *v1alpha1.OCIComponentVersionSource) componentVersionSource {
	return componentVersionSource{repository: &src.Repository, component: src.Component, version: src.Version, digest: src.Digest}
}

func ctfComponentVersionSource(src *v1alpha1.CTFComponentVersionSource) componentVersionSource {
	return componentVersionSource{repository: &src.Repository, component: src.Component, version: src.Version, digest: src.Digest}
}

// open opens the OCI graph of the component version identified by src, pinned to its digest if it
// has one. The returned graph reads from the source repository on demand.
func (src componentVersionSource) open(ctx context.Context, provider repository.ComponentVersionRepositoryProvider, resolver credentials.Resolver) (ocistream.ResourceStream, error) {
	if src.component == "" {
		return nil, fmt.Errorf("component name is required")
	}
	if src.version == "" {
		return nil, fmt.Errorf("component version is required")
	}
	repo, err := componentVersionRepository(ctx, provider, resolver, src.repository)
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	if repo, err = repository.PinRepository(repo, src.component, src.version, src.digest); err != nil {
		return nil, err
	}
	source, ok := repo.(componentGraphSource)
	if !ok {
		return nil, fmt.Errorf("source repository %T cannot provide the OCI graph of component version %s:%s", repo, src.component, src.version)
	}
	graph, err := source.DownloadComponentVersionGraph(ctx, src.component, src.version)
	if err != nil {
		return nil, fmt.Errorf("failed getting the OCI graph of component version %s:%s: %w", src.component, src.version, err)
	}
	return graph, nil
}
