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
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/registry"

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
	"ocm.software/open-component-model/bindings/go/oci/internal/looseref"
	"ocm.software/open-component-model/bindings/go/oci/internal/pack"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	descriptor2 "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	indexv1 "ocm.software/open-component-model/bindings/go/oci/spec/index/component/v1"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// LocalBlob represents a blob that is stored locally in the OCI repository.
// It provides methods to access the blob's metadata and content.
type LocalBlob fetch.LocalBlob

// ComponentVersionRepository defines the interface for storing and retrieving OCM component versions
// and their associated resources in a Store.
type ComponentVersionRepository interface {
	// AddComponentVersion adds a new component version to the repository.
	// If a component version already exists, it will be updated with the new descriptor.
	// The descriptor internally will be serialized via the runtime package.
	// The descriptor MUST have its target Name and Version already set as they are used to identify the target
	// Location in the Store.
	AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error

	// GetComponentVersion retrieves a component version from the repository.
	// Returns the descriptor from the most recent AddComponentVersion call for that component and version.
	GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error)

	// ListComponentVersions lists all component versions for a given component.
	// Returns a list of version strings, sorted on best effort by loose semver specification.
	// Note: Listing of Component Versions does not directly translate to an OCI Call.
	// Thus there are two approaches to list component versions:
	// - Listing all tags in the OCI repository and filtering them based on the resolved media type / artifact type
	// - Listing all referrers of the component index and filtering them based on the resolved media type / artifact type
	//
	// For more information on Referrer support, see
	// https://github.com/opencontainers/distribution-spec/blob/v1.1.0/spec.md#listing-referrers
	ListComponentVersions(ctx context.Context, component string) ([]string, error)

	// AddLocalResource adds a local resource to the repository.
	// The resource must be referenced in the component descriptor.
	// Resources for non-existent component versions may be stored but may be removed during garbage collection.
	// The Resource given is identified later on by its own Identity and a collection of a set of reserved identity values
	// that can have a special meaning.
	AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (newRes *descriptor.Resource, err error)

	// GetLocalResource retrieves a local resource from the repository.
	// The identity must match a resource in the component descriptor.
	GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (LocalBlob, *descriptor.Resource, error)
}

// ResourceRepository defines the interface for storing and retrieving OCM resources
// independently of component versions from a Store Implementation
type ResourceRepository interface {
	// UploadResource uploads a resource to the repository.
	// Returns the updated resource with repository-specific information.
	// The resource must be referenced in the component descriptor.
	// Note that UploadResource is special in that it considers both
	// - the Source Access from descriptor.Resource
	// - the Target Access from the given target specification
	// It might be that during the upload, the source pointer may be updated with information gathered during upload
	// (e.g. digest, size, etc).
	//
	// The content of form blob.ReadOnlyBlob is expected to be a (optionally gzipped) tar archive that can be read with
	// tar.ReadOCILayout, which interprets the blob as an OCILayout.
	//
	// The given OCI Layout MUST contain the resource described in source with an v1.OCIImage specification,
	// otherwise the upload will fail
	UploadResource(ctx context.Context, targetAccess runtime.Typed, source *descriptor.Resource, content blob.ReadOnlyBlob) (err error)

	// DownloadResource downloads a resource from the repository.
	// THe resource MUST contain a valid v1.OCIImage specification that exists in the Store.
	// Otherwise, the download will fail.
	//
	// The blob.ReadOnlyBlob returned will always be an OCI Layout, readable by oci.NewFromTar.
	// For more information on the download procedure, see NewOCILayoutWriter.
	DownloadResource(ctx context.Context, res *descriptor.Resource) (content blob.ReadOnlyBlob, err error)
}

// Resolver defines the interface for resolving references to OCI stores.
type Resolver interface {
	// StoreForReference resolves a reference to a Store.
	// Each reference can resolve to a different store.
	// Note that multiple component versions might share the same store
	StoreForReference(ctx context.Context, reference string) (spec.Store, error)

	// ComponentVersionReference returns a unique reference for a component version.
	ComponentVersionReference(component, version string) string

	// Reference resolves a reference string to a fmt.Stringer whose "native"
	// format represents a valid reference that can be used for a given store returned
	// by StoreForReference.
	Reference(reference string) (fmt.Stringer, error)
}

// Repository implements the ComponentVersionRepository interface using OCI registries.
// Each component may be stored in a separate OCI repository, but ultimately the storage is determined by the Resolver.
//
// This Repository implementation synchronizes OCI Manifests through the concepts of LocalResourceManifestCache.
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

	// localResourceManifestCache temporarily stores manifests for local resources until they are added to a component version.
	localResourceManifestCache cache.OCIDescriptorCache
	// localResourceLayerCache temporarily stores layers for local resources until they are added to a component version.
	localResourceLayerCache cache.OCIDescriptorCache

	// resolver resolves component version references to OCI stores.
	resolver Resolver

	// creatorAnnotation is the annotation used to identify the creator of the component version.
	// see OCMCreator for more information.
	creatorAnnotation string

	// ResourceCopyOptions are the options used for copying resources between stores.
	// These options are used in copyResource.
	resourceCopyOptions oras.CopyOptions
}

var _ ComponentVersionRepository = (*Repository)(nil)

// AddComponentVersion adds a new component version to the repository.
func (repo *Repository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) (err error) {
	component, version := descriptor.Component.Name, descriptor.Component.Version
	done := log.Operation(ctx, "add component version", slog.String("component", component), slog.String("version", version))
	defer done(err)

	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return err
	}

	manifest, err := AddDescriptorToStore(ctx, store, descriptor, AddDescriptorOptions{
		Scheme:                        repo.scheme,
		Author:                        repo.creatorAnnotation,
		AdditionalDescriptorManifests: repo.localResourceManifestCache.Get(reference),
		AdditionalLayers:              repo.localResourceLayerCache.Get(reference),
	})
	if err != nil {
		return fmt.Errorf("failed to add descriptor to store: %w", err)
	}

	// Tag the manifest with the reference
	if err := store.Tag(ctx, *manifest, version); err != nil {
		return fmt.Errorf("failed to tag manifest: %w", err)
	}
	// Cleanup local blob memory as all layers have been pushed
	repo.localResourceManifestCache.Delete(reference)
	repo.localResourceLayerCache.Delete(reference)

	return nil
}

func (repo *Repository) ListComponentVersions(ctx context.Context, component string) (_ []string, err error) {
	done := log.Operation(ctx, "list component versions",
		slog.String("component", component))
	defer done(err)

	_, store, err := repo.getStore(ctx, component, "latest")
	if err != nil {
		return nil, err
	}

	list, err := lister.New(store)
	if err != nil {
		return nil, fmt.Errorf("failed to create lister: %w", err)
	}

	return list.List(ctx, lister.Options{
		SortPolicy:   lister.SortPolicyLooseSemverDescending,
		LookupPolicy: lister.LookupPolicyReferrerWithTagFallback,
		TagListerOptions: lister.TagListerOptions{
			VersionResolver: complister.ReferenceTagVersionResolver(store),
		},
		ReferrerListerOptions: lister.ReferrerListerOptions{
			ArtifactType:    descriptor2.MediaTypeComponentDescriptorV2,
			Subject:         indexv1.Descriptor,
			VersionResolver: complister.ReferrerAnnotationVersionResolver(component),
		},
	})
}

// GetComponentVersion retrieves a component version from the repository.
func (repo *Repository) GetComponentVersion(ctx context.Context, component, version string) (desc *descriptor.Descriptor, err error) {
	done := log.Operation(ctx, "add local resource",
		slog.String("component", component),
		slog.String("version", version))
	defer done(err)

	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return nil, err
	}

	desc, _, _, err = getDescriptorFromStore(ctx, store, reference)
	return desc, err
}

// AddLocalResource adds a local resource to the repository.
func (repo *Repository) AddLocalResource(
	ctx context.Context,
	component, version string,
	resource *descriptor.Resource,
	b blob.ReadOnlyBlob,
) (_ *descriptor.Resource, err error) {
	done := log.Operation(ctx, "add local resource",
		slog.String("component", component),
		slog.String("version", version),
		log.IdentityLogAttr("resource", resource.ToIdentity()))
	defer done(err)

	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return nil, err
	}

	resourceBlob, err := ociblob.NewResourceBlob(resource, b)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource blob: %w", err)
	}

	desc, err := pack.ResourceBlob(ctx, store, resourceBlob, pack.Options{
		AccessScheme:     repo.scheme,
		CopyGraphOptions: repo.resourceCopyOptions.CopyGraphOptions,
		BaseReference:    reference,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to pack resource blob: %w", err)
	}

	if introspection.IsOCICompliantManifest(desc) {
		repo.localResourceManifestCache.Add(reference, desc)
	} else {
		repo.localResourceLayerCache.Add(reference, desc)
	}

	return resource, nil
}

// GetLocalResource retrieves a local resource from the repository.
func (repo *Repository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (LocalBlob, *descriptor.Resource, error) {
	var err error
	done := log.Operation(ctx, "get local resource",
		slog.String("component", component),
		slog.String("version", version),
		log.IdentityLogAttr("resource", identity))
	defer done(err)

	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return nil, nil, err
	}

	desc, manifest, index, err := getDescriptorFromStore(ctx, store, reference)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get component version: %w", err)
	}

	var candidates []descriptor.Resource
	for _, res := range desc.Component.Resources {
		if identity.Match(res.ElementMeta.ToIdentity(), runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
			candidates = append(candidates, res)
		}
	}
	if len(candidates) != 1 {
		return nil, nil, fmt.Errorf("found %d candidates while looking for resource %q, but expected exactly one", len(candidates), identity)
	}
	resource := candidates[0]
	log.Base().Info("found resource in descriptor", "resource", resource.ToIdentity())

	if resource.Access.GetType().IsEmpty() {
		return nil, nil, fmt.Errorf("resource access type is empty")
	}
	typed, err := repo.scheme.NewObject(resource.Access.GetType())
	if err != nil {
		return nil, nil, fmt.Errorf("error creating resource access: %w", err)
	}
	if err := repo.scheme.Convert(resource.Access, typed); err != nil {
		return nil, nil, fmt.Errorf("error converting resource access: %w", err)
	}

	switch typed := typed.(type) {
	case *v2.LocalBlob:
		return repo.getLocalBlobResource(ctx, store, index, manifest, resource, typed, identity)
	default:
		return nil, nil, fmt.Errorf("unsupported resource access type: %T", typed)
	}
}

func (repo *Repository) getLocalBlobResource(ctx context.Context, store spec.Store, index *ociImageSpecV1.Index, manifest *ociImageSpecV1.Manifest, resource descriptor.Resource, typed *v2.LocalBlob, identity runtime.Identity) (LocalBlob, *descriptor.Resource, error) {
	// if the index does not exist, we can only use the manifest
	// and thus local blobs can only be available as image layers
	if index == nil {
		b, err := fetch.SingleLayerLocalBlobFromManifestByIdentity(ctx, store, manifest, identity)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get local blob from manifest: %w", err)
		}
		return b, &resource, nil
	}

	// lets lookup our artifact based on our annotation from either the index or the manifest layers.
	artifact, err := annotations.FilterFirstMatchingArtifact(append(index.Manifests, manifest.Layers...), identity, annotations.ArtifactKindResource)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find matching descriptor: %w", err)
	}

	// if we are not a manifest compatible with OCI, we can assume that we only care about a single layer
	// that means we can just return the blob that contains that exact layer and not the entire oci layout
	if !introspection.IsOCICompliantManifest(artifact) {
		// Validate the digest of the downloaded content matches what we expect
		if err := validateDigest(&resource, artifact.Digest); err != nil {
			return nil, nil, fmt.Errorf("failed to validate digest: %w", err)
		}

		layerData, err := store.Fetch(ctx, artifact)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch layer data: %w", err)
		}

		b := ociblob.NewDescriptorBlob(layerData, artifact)

		if actualDigest, _ := b.Digest(); actualDigest != artifact.Digest.String() {
			return nil, nil, fmt.Errorf("expected single layer artifact digest %q but got %q", artifact.Digest.String(), actualDigest)
		}
		return b, &resource, nil
	}

	// if we dont have a single layer we have to copy not only the manifest or index, but all layers that are part of it!
	b, err := generateOCILayout(ctx, &resource, store, artifact, repo.resourceCopyOptions.CopyGraphOptions)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get local blob from discovered image layout: %w", err)
	}
	return b, &resource, nil
}

func (repo *Repository) getStore(ctx context.Context, component string, version string) (ref string, store spec.Store, err error) {
	reference := repo.resolver.ComponentVersionReference(component, version)
	if store, err = repo.resolver.StoreForReference(ctx, reference); err != nil {
		return "", nil, fmt.Errorf("failed to get store for reference: %w", err)
	}
	return reference, store, nil
}

// UploadResource uploads a resource to the repository.
func (repo *Repository) UploadResource(ctx context.Context, target runtime.Typed, res *descriptor.Resource, b blob.ReadOnlyBlob) (err error) {
	done := log.Operation(ctx, "upload resource", log.IdentityLogAttr("resource", res.ToIdentity()))
	defer done(err)

	var old accessv1.OCIImage
	if err := repo.scheme.Convert(res.Access, &old); err != nil {
		return fmt.Errorf("error converting resource old to OCI image: %w", err)
	}

	var access accessv1.OCIImage
	if err := repo.scheme.Convert(target, &access); err != nil {
		return fmt.Errorf("error converting resource target to OCI image: %w", err)
	}

	store, err := repo.resolver.StoreForReference(ctx, access.ImageReference)
	if err != nil {
		return err
	}

	ociStore, err := tar.ReadOCILayout(ctx, b)
	if err != nil {
		return fmt.Errorf("failed to read OCI layout: %w", err)
	}
	defer func() {
		err = errors.Join(err, ociStore.Close())
	}()

	// Handle non-absolute reference names for OCI Layouts
	// This is a workaround for the fact that some tools like ORAS CLI
	// can generate OCI Layouts that contain relative reference names, aka only tags
	// and not absolute references.
	//
	// An example would be ghcr.io/test:v1.0.0
	// This could get stored in an OCI Layout as
	// v1.0.0 only, assuming that it is the only repository in the OCI Layout.
	srcRef := old.ImageReference
	if _, err := ociStore.Resolve(ctx, srcRef); err != nil {
		parsedSrcRef, pErr := registry.ParseReference(srcRef)
		if pErr != nil {
			return errors.Join(err, pErr)
		}
		if _, rErr := ociStore.Resolve(ctx, parsedSrcRef.Reference); rErr != nil {
			return errors.Join(err, rErr)
		}
		slog.Info("resolved non-absolute reference name from oci layout", "old", srcRef, "new", parsedSrcRef.Reference)
		srcRef = parsedSrcRef.Reference
	}

	ref, err := looseref.ParseReference(access.ImageReference)
	if err != nil {
		return fmt.Errorf("failed to parse target access image reference %q: %w", access.ImageReference, err)
	}
	if err := ref.ValidateReferenceAsTag(); err != nil {
		return fmt.Errorf("can only copy %q if it is tagged: %w", access.ImageReference, err)
	}

	tag := ref.Tag

	desc, err := oras.Copy(ctx, ociStore, srcRef, store, tag, repo.resourceCopyOptions)
	if err != nil {
		return fmt.Errorf("failed to upload resource via copy: %w", err)
	}

	res.Size = desc.Size
	if err := internaldigest.ApplyToResource(res, desc.Digest); err != nil {
		return fmt.Errorf("failed to apply digest to resource: %w", err)
	}
	res.Access = &access

	return nil
}

// DownloadResource downloads a resource from the repository.
func (repo *Repository) DownloadResource(ctx context.Context, res *descriptor.Resource) (data blob.ReadOnlyBlob, err error) {
	done := log.Operation(ctx, "download resource", log.IdentityLogAttr("resource", res.ToIdentity()))
	defer done(err)

	if res.Access.GetType().IsEmpty() {
		return nil, fmt.Errorf("resource access type is empty")
	}
	typed, err := repo.scheme.NewObject(res.Access.GetType())
	if err != nil {
		return nil, fmt.Errorf("error creating resource access: %w", err)
	}
	if err := repo.scheme.Convert(res.Access, typed); err != nil {
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
		res := res.DeepCopy()
		res.Access = globalAccess
		return repo.DownloadResource(ctx, res)
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

		return generateOCILayout(ctx, res, src, desc, repo.resourceCopyOptions.CopyGraphOptions, typed.ImageReference)
	default:
		return nil, fmt.Errorf("unsupported resource access type: %T", typed)
	}
}

// generateOCILayout creates an OCI layout from a store for a given descriptor.
func generateOCILayout(ctx context.Context, res *descriptor.Resource, src spec.Store, desc ociImageSpecV1.Descriptor, copyOpts oras.CopyGraphOptions, tags ...string) (_ LocalBlob, err error) {
	downloaded, err := tar.CopyToOCILayoutInMemory(ctx, src, desc, tar.CopyToOCILayoutOptions{
		CopyGraphOptions: copyOpts,
		Tags:             tags,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to copy to OCI layout: %w", err)
	}

	if err := validateDigest(res, desc.Digest); err != nil {
		return nil, fmt.Errorf("digest validation failed: %w", err)
	}

	return downloaded, nil
}

func validateDigest(res *descriptor.Resource, blobDigest digest.Digest) error {
	if res.Digest == nil {
		// the resource does not have a digest, so we cannot validate it
		return nil
	}

	expected := digest.NewDigestFromEncoded(internaldigest.SHAMapping[res.Digest.HashAlgorithm], res.Digest.Value)

	var actual digest.Digest
	switch res.Digest.NormalisationAlgorithm {
	case "genericBlobDigest/v1":
		// the digest is based on the entire blob
		actual = blobDigest
	default:
		return fmt.Errorf("unsupported digest algorithm: %s", res.Digest.NormalisationAlgorithm)
	}
	if expected != actual {
		return fmt.Errorf("expected resource digest %q to equal downloaded descriptor digest %q", expected, actual)
	}

	return nil
}

// getDescriptorOCIImageManifest retrieves the manifest for a given reference from the store.
// It handles both OCI image indexes and OCI image manifests.
func getDescriptorOCIImageManifest(ctx context.Context, store spec.Store, reference string) (manifest ociImageSpecV1.Manifest, index *ociImageSpecV1.Index, err error) {
	log.Base().Log(ctx, slog.LevelInfo, "resolving descriptor", slog.String("reference", reference))
	base, err := store.Resolve(ctx, reference)
	if err != nil {
		return ociImageSpecV1.Manifest{}, nil, fmt.Errorf("failed to resolve reference %q: %w", reference, err)
	}
	log.Base().Log(ctx, slog.LevelInfo, "fetching descriptor", log.DescriptorLogAttr(base))
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
