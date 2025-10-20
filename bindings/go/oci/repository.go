// Package oci provides functionality for storing and retrieving Open Component Model (OCM) components
// using the Open Container Initiative (OCI) registry format. It implements the OCM repository interface
// using OCI registries as the underlying storage mechanism.

package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	slogcontext "github.com/veqryn/slog-context"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/errdef"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	internaldigest "ocm.software/open-component-model/bindings/go/oci/internal/digest"
	"ocm.software/open-component-model/bindings/go/oci/internal/fetch"
	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
	"ocm.software/open-component-model/bindings/go/oci/internal/lister"
	complister "ocm.software/open-component-model/bindings/go/oci/internal/lister/component"
	"ocm.software/open-component-model/bindings/go/oci/internal/log"
	"ocm.software/open-component-model/bindings/go/oci/internal/pack"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	descriptor2 "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	indexv1 "ocm.software/open-component-model/bindings/go/oci/spec/index/component/v1"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ ComponentVersionRepository = (*Repository)(nil)

// Repository implements the ComponentVersionRepository interface using OCI registries.
// Each component may be stored in a separate OCI repository, but ultimately the storage is determined by the Resolver.
//
// This Repository implementation synchronizes OCI Manifests through the concepts of LocalManifestCache.
// Through this any local blob added with AddLocalResource will be added to the memory until
// AddComponentVersion is called with a reference to that resource.
// This allows the repository to associate newly added blobs with the component version and still upload them
// when AddLocalResource is called.
//
// Note: Store implementations are expected to either allow orphaned local resources or
// regularly issue an async garbage collection to remove them due to this behavior.
// This however should not be an issue since all OCI registries implement such a garbage collection mechanism.
type Repository struct {
	scheme *runtime.Scheme

	// localArtifactManifestCache temporarily stores manifests for local artifacts until they are added to a component version.
	localArtifactManifestCache cache.OCIDescriptorCache
	// localArtifactLayerCache temporarily stores layers for local artifacts until they are added to a component version.
	localArtifactLayerCache cache.OCIDescriptorCache

	// resolver resolves component version references to OCI stores.
	resolver Resolver

	// creatorAnnotation is the annotation used to identify the creator of the component version.
	// see OCMCreator for more information.
	creatorAnnotation string

	// ResourceCopyOptions are the options used for copying resources between stores.
	// These options are used in copyResource.
	resourceCopyOptions oras.CopyOptions

	// referrerTrackingPolicy defines how OCI referrers are used to track component versions.
	referrerTrackingPolicy ReferrerTrackingPolicy

	// logger is the logger used for OCI operations.
	logger *slog.Logger

	// the media type of the descriptor encoding used for component versions.
	// this is used to determine the media type of the component descriptor when adding new component versions.
	descriptorEncodingMediaType string
}

// AddComponentVersion adds a new component version to the repository.
func (repo *Repository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) (err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	component, version := descriptor.Component.Name, descriptor.Component.Version
	done := log.Operation(ctx, "add component version", slog.String("component", component), slog.String("version", version))
	defer func() {
		done(err)
	}()

	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return err
	}

	manifest, err := AddDescriptorToStore(ctx, store, descriptor, AddDescriptorOptions{
		Scheme:                        repo.scheme,
		Author:                        repo.creatorAnnotation,
		AdditionalDescriptorManifests: repo.localArtifactManifestCache.Get(reference),
		AdditionalLayers:              repo.localArtifactLayerCache.Get(reference),
		ReferrerTrackingPolicy:        repo.referrerTrackingPolicy,
		DescriptorEncodingMediaType:   repo.descriptorEncodingMediaType,
	})
	if err != nil {
		return fmt.Errorf("failed to add descriptor to store: %w", err)
	}

	if err := store.Tag(ctx, *manifest, reference); err != nil {
		return fmt.Errorf("failed to tag manifest: %w", err)
	}
	// Cleanup local blob memory as all layers have been pushed
	repo.localArtifactManifestCache.Delete(reference)
	repo.localArtifactLayerCache.Delete(reference)

	return nil
}

func (repo *Repository) ListComponentVersions(ctx context.Context, component string) (_ []string, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "list component versions",
		slog.String("component", component))
	defer func() {
		done(err)
	}()

	_, store, err := repo.getStore(ctx, component, "latest")
	if err != nil {
		return nil, err
	}

	list, err := lister.New(store)
	if err != nil {
		return nil, fmt.Errorf("failed to create lister: %w", err)
	}

	opts := lister.Options{
		SortPolicy: lister.SortPolicyLooseSemverDescending,
		TagListerOptions: lister.TagListerOptions{
			VersionResolver: complister.ReferenceTagVersionResolver(component, store),
		},
		ReferrerListerOptions: lister.ReferrerListerOptions{
			ArtifactType:    descriptor2.MediaTypeComponentDescriptorV2,
			Subject:         indexv1.Descriptor,
			VersionResolver: complister.ReferrerAnnotationVersionResolver(component),
		},
	}

	switch repo.referrerTrackingPolicy {
	case ReferrerTrackingPolicyByIndexAndSubject:
		opts.LookupPolicy = lister.LookupPolicyReferrerWithTagFallback
	case ReferrerTrackingPolicyNone:
		opts.LookupPolicy = lister.LookupPolicyTagOnly
	}

	return list.List(ctx, opts)
}

// CheckHealth checks if the repository is accessible and properly configured.
func (repo *Repository) CheckHealth(ctx context.Context) (err error) {
	return repo.resolver.Ping(slogcontext.NewCtx(ctx, repo.logger))
}

// GetComponentVersion retrieves a component version from the repository.
func (repo *Repository) GetComponentVersion(ctx context.Context, component, version string) (desc *descriptor.Descriptor, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "get component version",
		slog.String("component", component),
		slog.String("version", version))
	defer func() {
		done(err)
	}()

	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return nil, err
	}

	desc, _, _, err = getDescriptorFromStore(ctx, store, reference)
	if errors.Is(err, errdef.ErrNotFound) {
		return desc, errors.Join(repository.ErrNotFound, fmt.Errorf("component version %s/%s not found: %w", component, version, err))
	}
	return desc, err
}

// AddLocalResource adds a local resource to the repository.
func (repo *Repository) AddLocalResource(
	ctx context.Context,
	component, version string,
	resource *descriptor.Resource,
	b blob.ReadOnlyBlob,
) (_ *descriptor.Resource, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "add local resource",
		slog.String("component", component),
		slog.String("version", version),
		log.IdentityLogAttr("resource", resource.ToIdentity()))
	defer func() {
		done(err)
	}()

	resource = resource.DeepCopy()

	if err := repo.uploadAndUpdateLocalArtifact(ctx, component, version, resource, b); err != nil {
		return nil, err
	}

	return resource, nil
}

func (repo *Repository) AddLocalSource(ctx context.Context, component, version string, source *descriptor.Source, content blob.ReadOnlyBlob) (newRes *descriptor.Source, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "add local source",
		slog.String("component", component),
		slog.String("version", version),
		log.IdentityLogAttr("source", source.ToIdentity()))
	defer func() {
		done(err)
	}()

	source = source.DeepCopy()

	if err := repo.uploadAndUpdateLocalArtifact(ctx, component, version, source, content); err != nil {
		return nil, err
	}

	return source, nil
}

func (repo *Repository) ProcessResourceDigest(ctx context.Context, res *descriptor.Resource) (_ *descriptor.Resource, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "process resource digest",
		log.IdentityLogAttr("resource", res.ToIdentity()))
	defer func() {
		done(err)
	}()
	res = res.DeepCopy()
	switch typed := res.Access.(type) {
	case *v2.LocalBlob:
		if typed.GlobalAccess == nil {
			return nil, fmt.Errorf("local blob access does not have a global access and cannot be used")
		}
		globalAccess, err := repo.scheme.NewObject(typed.GlobalAccess.GetType())
		if err != nil {
			return nil, fmt.Errorf("error creating typed global blob access with help of scheme: %w", err)
		}
		if err := repo.scheme.Convert(typed.GlobalAccess, globalAccess); err != nil {
			return nil, fmt.Errorf("error converting global blob access: %w", err)
		}
		res.Access = globalAccess
		return repo.ProcessResourceDigest(ctx, res)
	case *accessv1.OCIImage:
		return repo.processOCIImageDigest(ctx, res, typed)
	default:
		return nil, fmt.Errorf("unsupported resource access type: %T", typed)
	}
}

func (repo *Repository) processOCIImageDigest(ctx context.Context, res *descriptor.Resource, typed *accessv1.OCIImage) (*descriptor.Resource, error) {
	src, err := repo.resolver.StoreForReference(ctx, typed.ImageReference)
	if err != nil {
		return nil, err
	}

	resolved, err := repo.resolver.Reference(typed.ImageReference)
	if err != nil {
		return nil, fmt.Errorf("error parsing image reference %q: %w", typed.ImageReference, err)
	}

	reference := resolved.String()

	// reference is not a FQDN because it can be pinned, for resolving, use the FQDN part of the reference
	fqdn := reference
	pinnedDigest := ""
	if index := strings.IndexByte(reference, '@'); index != -1 {
		fqdn = reference[:index]
		pinnedDigest = reference[index+1:]
	}

	var desc ociImageSpecV1.Descriptor
	if desc, err = src.Resolve(ctx, fqdn); err != nil {
		return nil, fmt.Errorf("failed to resolve reference to process digest %q: %w", typed.ImageReference, err)
	}

	// if the resource did not have a digest, we apply the digest from the descriptor
	// if it did, we verify it against the received descriptor.
	if res.Digest == nil {
		res.Digest = &descriptor.Digest{}
		if err := internaldigest.Apply(res.Digest, desc.Digest); err != nil {
			return nil, fmt.Errorf("failed to apply digest to resource: %w", err)
		}
	} else if err := internaldigest.Verify(res.Digest, desc.Digest); err != nil {
		return nil, fmt.Errorf("failed to verify digest of resource %q: %w", res.ToIdentity(), err)
	}

	if pinnedDigest != "" && pinnedDigest != desc.Digest.String() {
		return nil, fmt.Errorf("expected pinned digest %q (derived from %q) but got %q", pinnedDigest, reference, desc.Digest)
	}

	// in any case, after successful processing, we can pin the access
	typed.ImageReference = fqdn + "@" + desc.Digest.String()
	res.Access = typed

	return res, nil
}

func (repo *Repository) uploadAndUpdateLocalArtifact(ctx context.Context, component string, version string, artifact descriptor.Artifact, b blob.ReadOnlyBlob) error {
	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return err
	}

	if err := ociblob.UpdateArtifactWithInformationFromBlob(artifact, b); err != nil {
		return fmt.Errorf("failed to update artifact with data from blob: %w", err)
	}

	artifactBlob, err := ociblob.NewArtifactBlob(artifact, b)
	if err != nil {
		return fmt.Errorf("failed to create resource blob: %w", err)
	}

	desc, err := pack.ArtifactBlob(ctx, store, artifactBlob, pack.Options{
		AccessScheme:     repo.scheme,
		CopyGraphOptions: repo.resourceCopyOptions.CopyGraphOptions,
		BaseReference:    reference,
	})
	if err != nil {
		return fmt.Errorf("failed to pack resource blob: %w", err)
	}

	if introspection.IsOCICompliantManifest(desc) {
		repo.localArtifactManifestCache.Add(reference, desc)
	} else {
		repo.localArtifactLayerCache.Add(reference, desc)
	}

	return nil
}

// GetLocalResource retrieves a local resource from the repository.
func (repo *Repository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	var err error
	done := log.Operation(ctx, "get local resource",
		slog.String("component", component),
		slog.String("version", version),
		log.IdentityLogAttr("resource", identity))
	defer func() {
		done(err)
	}()

	var b fetch.LocalBlob
	var artifact descriptor.Artifact
	if b, artifact, err = repo.localArtifact(ctx, component, version, identity, annotations.ArtifactKindResource); err != nil {
		if errors.Is(err, errdef.ErrNotFound) {
			return nil, nil, errors.Join(repository.ErrNotFound, fmt.Errorf("component version %s/%s not found: %w", component, version, err))
		}
		return nil, nil, err
	}
	return b, artifact.(*descriptor.Resource), nil
}

func (repo *Repository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	var err error
	done := log.Operation(ctx, "get local source",
		slog.String("component", component),
		slog.String("version", version),
		log.IdentityLogAttr("resource", identity))
	defer func() {
		done(err)
	}()

	var b fetch.LocalBlob
	var artifact descriptor.Artifact
	if b, artifact, err = repo.localArtifact(ctx, component, version, identity, annotations.ArtifactKindSource); err != nil {
		if errors.Is(err, errdef.ErrNotFound) {
			return nil, nil, errors.Join(repository.ErrNotFound, fmt.Errorf("component version %s/%s not found: %w", component, version, err))
		}
		return nil, nil, err
	}
	return b, artifact.(*descriptor.Source), nil
}

func (repo *Repository) localArtifact(ctx context.Context, component, version string, identity runtime.Identity, kind annotations.ArtifactKind) (fetch.LocalBlob, descriptor.Artifact, error) {
	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return nil, nil, err
	}
	desc, manifest, index, err := getDescriptorFromStore(ctx, store, reference)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get component version: %w", err)
	}

	var candidates []descriptor.Artifact
	switch kind {
	case annotations.ArtifactKindResource:
		for _, res := range desc.Component.Resources {
			if identity.Match(res.ToIdentity(), runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
				candidates = append(candidates, &res)
			}
		}
	case annotations.ArtifactKindSource:
		for _, src := range desc.Component.Sources {
			if identity.Match(src.ToIdentity(), runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
				candidates = append(candidates, &src)
			}
		}
	}
	if len(candidates) != 1 {
		return nil, nil, fmt.Errorf("found %d candidates while looking for %s %q, but expected exactly one", len(candidates), kind, identity)
	}
	artifact := candidates[0]
	meta := artifact.GetElementMeta()

	// now that we have a unique candidate, we should use its identity instead of the one requested, as
	// the requested identity might not be fully qualified.
	// For example, it is valid to ask for "name=abc", but receive an artifact with "name=abc,version=1.0.0".
	slogcontext.Info(ctx, "found artifact in descriptor", "artifact", meta.ToIdentity())

	access := artifact.GetAccess()
	typed, err := repo.scheme.NewObject(access.GetType())
	if err != nil {
		return nil, nil, fmt.Errorf("error creating resource access: %w", err)
	}
	if err := repo.scheme.Convert(access, typed); err != nil {
		return nil, nil, fmt.Errorf("error converting resource access: %w", err)
	}

	switch typed := typed.(type) {
	case *v2.LocalBlob:
		b, err := repo.getLocalBlobFromIndexOrManifest(ctx, store, index, manifest, typed.LocalReference)
		return b, artifact, err
	default:
		return nil, nil, fmt.Errorf("unsupported resource access type: %T", typed)
	}
}

// getLocalBlobFromIndexOrManifest resolves and fetches a blob from either an
// OCI index or a manifest. It looks up the descriptor matching the given
// reference and then:
func (repo *Repository) getLocalBlobFromIndexOrManifest(
	ctx context.Context,
	store spec.Store,
	index *ociImageSpecV1.Index,
	manifest *ociImageSpecV1.Manifest,
	ref string,
) (LocalBlob, error) {
	descriptors := collectDescriptors(index, manifest)

	artifact, err := findDescriptorFromReference(descriptors, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact %q: %w", ref, err)
	}

	// Nested manifest: copy full OCI layout
	if index != nil && introspection.IsOCICompliantManifest(artifact) {
		// copy the full OCI manifest and its dependency graph
		// into an in-memory OCI layout. This is used when the descriptor refers
		// to another OCI-compliant manifest instead of a single layer.
		return tar.CopyToOCILayoutInMemory(ctx, store, artifact, tar.CopyToOCILayoutOptions{
			CopyGraphOptions: repo.resourceCopyOptions.CopyGraphOptions,
		})
	}

	// Fetch a single layer blob from the store and verify
	// that its digest matches the expected descriptor digest. This path is used
	// when the reference is a raw layer rather than a manifest.
	data, err := store.Fetch(ctx, artifact)
	if err != nil {
		return nil, fmt.Errorf("fetch layer: %w", err)
	}

	b := ociblob.NewDescriptorBlob(data, artifact)
	if actual, _ := b.Digest(); actual != artifact.Digest.String() {
		return nil, fmt.Errorf("digest mismatch: expected %q, got %q", artifact.Digest, actual)
	}
	return b, nil
}

func (repo *Repository) getStore(ctx context.Context, component string, version string) (ref string, store spec.Store, err error) {
	reference := repo.resolver.ComponentVersionReference(ctx, component, version)
	if store, err = repo.resolver.StoreForReference(ctx, reference); err != nil {
		return "", nil, fmt.Errorf("failed to get store for reference: %w", err)
	}
	return reference, store, nil
}

// UploadResource uploads a [*descriptor.Resource] to the repository.
func (repo *Repository) UploadResource(ctx context.Context, res *descriptor.Resource, b blob.ReadOnlyBlob) (newRes *descriptor.Resource, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "upload resource", log.IdentityLogAttr("resource", res.ToIdentity()))
	defer func() {
		done(err)
	}()

	res = res.DeepCopy()

	desc, access, err := repo.uploadOCIImage(ctx, res.Access, b)
	if err != nil {
		return nil, fmt.Errorf("failed to upload resource as OCI image: %w", err)
	}

	if res.Digest == nil {
		res.Digest = &descriptor.Digest{}
	}
	if err := internaldigest.Apply(res.Digest, desc.Digest); err != nil {
		return nil, fmt.Errorf("failed to apply digest to resource: %w", err)
	}
	res.Access = access

	return res, nil
}

// UploadSource uploads a [*descriptor.Source] to the repository.
func (repo *Repository) UploadSource(ctx context.Context, src *descriptor.Source, b blob.ReadOnlyBlob) (newSrc *descriptor.Source, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "upload source", log.IdentityLogAttr("source", src.ToIdentity()))
	defer func() {
		done(err)
	}()

	src = src.DeepCopy()

	_, access, err := repo.uploadOCIImage(ctx, src.Access, b)
	if err != nil {
		return nil, fmt.Errorf("failed to upload source as OCI image: %w", err)
	}
	src.Access = access

	return src, nil
}

func (repo *Repository) uploadOCIImage(ctx context.Context, newAccess runtime.Typed, b blob.ReadOnlyBlob) (_ ociImageSpecV1.Descriptor, _ *accessv1.OCIImage, err error) {
	var access accessv1.OCIImage
	if err := repo.scheme.Convert(newAccess, &access); err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("error converting resource target to OCI image: %w", err)
	}

	store, err := repo.resolver.StoreForReference(ctx, access.ImageReference)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, err
	}

	ociStore, err := tar.ReadOCILayout(ctx, b)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to read OCI layout: %w", err)
	}
	defer func() {
		err = errors.Join(err, ociStore.Close())
	}()

	mainArtifacts := ociStore.MainArtifacts(ctx)
	if len(mainArtifacts) != 1 {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("expected exactly one main artifact in OCI layout, but got %d", len(mainArtifacts))
	}
	main := mainArtifacts[0]

	ref, err := looseref.ParseReference(access.ImageReference)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to parse target access image reference %q: %w", access.ImageReference, err)
	}
	if err := ref.ValidateReferenceAsTag(); err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("can only copy %q if it is tagged: %w", access.ImageReference, err)
	}

	if err := oras.CopyGraph(ctx, ociStore, store, main, repo.resourceCopyOptions.CopyGraphOptions); err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to upload resource via copy: %w", err)
	}

	if err := store.Tag(ctx, main, ref.Tag); err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to tag main artifact with tag %q: %w", ref.Tag, err)
	}

	return main, &access, nil
}

// DownloadResource downloads a [*descriptor.Resource] from the repository.
func (repo *Repository) DownloadResource(ctx context.Context, res *descriptor.Resource) (data blob.ReadOnlyBlob, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "download resource", log.IdentityLogAttr("resource", res.ToIdentity()))
	defer func() {
		done(err)
	}()

	if res.Access.GetType().IsEmpty() {
		return nil, fmt.Errorf("resource access type is empty")
	}
	return repo.download(ctx, res.Access)
}

// DownloadSource downloads a [*descriptor.Source] from the repository.
func (repo *Repository) DownloadSource(ctx context.Context, src *descriptor.Source) (data blob.ReadOnlyBlob, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "download source", log.IdentityLogAttr("resource", src.ToIdentity()))
	defer func() {
		done(err)
	}()

	if src.Access.GetType().IsEmpty() {
		return nil, fmt.Errorf("source access type is empty")
	}
	return repo.download(ctx, src.Access)
}

// download downloads an artifact specified by an access from the repository into a blob.ReadOnlyBlob.
// It is expected that the access is
//   - a valid [accessv1.OCIImage], or
//   - a [v2.LocalBlob] access that has a [v2.LocalBlob.GlobalAccess] set that can be interpreted as [accessv1.OCIImage].
func (repo *Repository) download(ctx context.Context, access runtime.Typed) (data blob.ReadOnlyBlob, err error) {
	typed, err := repo.scheme.NewObject(access.GetType())
	if err != nil {
		return nil, fmt.Errorf("error creating resource access: %w", err)
	}
	if err := repo.scheme.Convert(access, typed); err != nil {
		return nil, fmt.Errorf("error converting resource access: %w", err)
	}

	switch typed := typed.(type) {
	case *v2.LocalBlob:
		if typed.GlobalAccess == nil {
			return nil, fmt.Errorf("local blob access does not have a global access and cannot be used")
		}

		globalAccess, err := repo.scheme.NewObject(typed.GlobalAccess.GetType())
		if err != nil {
			return nil, fmt.Errorf("error creating typed global blob access with help of scheme: %w", err)
		}
		if err := repo.scheme.Convert(typed.GlobalAccess, globalAccess); err != nil {
			return nil, fmt.Errorf("error converting global blob access: %w", err)
		}
		return repo.download(ctx, globalAccess)
	case *accessv1.OCIImage:
		src, err := repo.resolver.StoreForReference(ctx, typed.ImageReference)
		if err != nil {
			return nil, err
		}

		resolved, err := repo.resolver.Reference(typed.ImageReference)
		if err != nil {
			return nil, fmt.Errorf("error parsing image reference %q: %w", typed.ImageReference, err)
		}

		reference := resolved.String()

		// reference is not a FQDN
		if index := strings.IndexByte(reference, '@'); index != -1 {
			reference = reference[index+1:]
		}

		desc, err := src.Resolve(ctx, reference)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve reference %q: %w", typed.ImageReference, err)
		}

		downloaded, err := tar.CopyToOCILayoutInMemory(ctx, src, desc, tar.CopyToOCILayoutOptions{
			CopyGraphOptions: repo.resourceCopyOptions.CopyGraphOptions,
			Tags:             []string{typed.ImageReference},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to copy to OCI layout: %w", err)
		}

		return downloaded, nil
	default:
		return nil, fmt.Errorf("unsupported resource access type: %T", typed)
	}
}

// getDescriptorOCIImageManifest retrieves the manifest for a given reference from the store.
// It handles both OCI image indexes and OCI image manifests.
func getDescriptorOCIImageManifest(ctx context.Context, store spec.Store, reference string) (manifest ociImageSpecV1.Manifest, index *ociImageSpecV1.Index, err error) {
	slogcontext.Log(ctx, slog.LevelDebug, "resolving descriptor", slog.String("reference", reference))
	base, err := store.Resolve(ctx, reference)
	if err != nil {
		return ociImageSpecV1.Manifest{}, nil, fmt.Errorf("failed to resolve reference %q: %w", reference, err)
	}
	slogcontext.Log(ctx, slog.LevelInfo, "fetching descriptor", log.DescriptorLogAttr(base))
	manifestRaw, err := store.Fetch(ctx, ociImageSpecV1.Descriptor{
		MediaType: base.MediaType,
		Digest:    base.Digest,
		Size:      base.Size,
	})
	if err != nil {
		return ociImageSpecV1.Manifest{}, nil, err
	}
	defer func() {
		err = errors.Join(err, manifestRaw.Close())
	}()

	switch base.MediaType {
	case ociImageSpecV1.MediaTypeImageIndex:
		if err := json.NewDecoder(manifestRaw).Decode(&index); err != nil {
			return ociImageSpecV1.Manifest{}, nil, err
		}
		if len(index.Manifests) == 0 {
			return ociImageSpecV1.Manifest{}, nil, fmt.Errorf("index has no manifests")
		}
		descriptorManifest := index.Manifests[0]
		if descriptorManifest.MediaType != ociImageSpecV1.MediaTypeImageManifest {
			return ociImageSpecV1.Manifest{}, nil, fmt.Errorf("index manifest is not an OCI image manifest")
		}
		manifestRaw, err = store.Fetch(ctx, descriptorManifest)
		if err != nil {
			return ociImageSpecV1.Manifest{}, nil, fmt.Errorf("failed to fetch manifest: %w", err)
		}
	case ociImageSpecV1.MediaTypeImageManifest:
	default:
		return ociImageSpecV1.Manifest{}, nil, fmt.Errorf("unsupported media type %q", base.MediaType)
	}

	if err := json.NewDecoder(manifestRaw).Decode(&manifest); err != nil {
		return ociImageSpecV1.Manifest{}, nil, err
	}
	return manifest, index, nil
}

func collectDescriptors(index *ociImageSpecV1.Index, manifest *ociImageSpecV1.Manifest) []ociImageSpecV1.Descriptor {
	if index == nil {
		return manifest.Layers
	}
	descs := make([]ociImageSpecV1.Descriptor, 0, len(index.Manifests)+len(manifest.Layers))
	descs = append(descs, index.Manifests...)
	descs = append(descs, manifest.Layers...)
	return descs
}

func findDescriptorFromReference(descriptors []ociImageSpecV1.Descriptor, reference string) (ociImageSpecV1.Descriptor, error) {
	asDigest := digest.Digest(reference)
	if err := asDigest.Validate(); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to validate reference %q as digest: %w", reference, err)
	}

	for _, desc := range descriptors {
		if desc.Digest == asDigest {
			return desc, nil
		}
	}
	return ociImageSpecV1.Descriptor{}, fmt.Errorf("no matching descriptor found for reference %s", reference)
}
