// Package oci provides functionality for storing and retrieving Open Component Model (OCM) components
// using the Open Container Initiative (OCI) registry format. It implements the OCM repository interface
// using OCI registries as the underlying storage mechanism.

package oci

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/errgroup"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	v1 "ocm.software/open-component-model/bindings/go/oci/access/v1"
	ociDigestV1 "ocm.software/open-component-model/bindings/go/oci/digest/v1"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Annotations for OCI Image Manifests
const (
	// AnnotationOCMComponentVersion is an annotation that indicates the component version.
	// It is an annotation that is used by OCM for a long time and is mainly set for compatibility reasons.
	// It does not serve any semantic meaning beyond declaring the component version with a fixed
	// prefix.
	AnnotationOCMComponentVersion = "software.ocm.componentversion"

	// AnnotationOCMCreator is an annotation that indicates the creator of the component version.
	// It is used historically by the OCM CLI to indicate the creator of the component version.
	// It is usually only a meta information, and has no semantic meaning beyond identifying a creating
	// process or user agent. as such it CAN be correlated to a user agent header in http.
	AnnotationOCMCreator = "software.ocm.creator"
)

// Media type constants for component descriptors
const (
	// MediaTypeComponentDescriptor is the base media type for OCM component descriptors
	MediaTypeComponentDescriptor = "application/vnd.ocm.software.component-descriptor"
	// MediaTypeComponentDescriptorV2 is the media type for version 2 of OCM component descriptors
	MediaTypeComponentDescriptorV2 = MediaTypeComponentDescriptor + ".v2"
)

// Media type constants for OCI image layouts
const (
	// MediaTypeOCIImageLayout is the media type for a complete OCI image layout
	// as per https://github.com/opencontainers/image-spec/blob/main/image-layout.md#oci-layout-file
	MediaTypeOCIImageLayout = "application/vnd.ocm.software.oci.layout"
	// MediaTypeOCIImageLayoutV1 is the media type for version 1 of OCI image layouts
	MediaTypeOCIImageLayoutV1 = MediaTypeOCIImageLayout + ".v1"
)

var logger = slog.With(slog.String("realm", "oci"))

// LocalBlob represents a blob that is stored locally in the OCI repository.
// It provides methods to access the blob's metadata and content.
type LocalBlob interface {
	blob.ReadOnlyBlob
	blob.SizeAware
	blob.DigestAware
	blob.MediaTypeAware
}

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

	// AddLocalResource adds a local resource to the repository.
	// The resource must be referenced in the component descriptor.
	// Resources for non-existent component versions may be stored but may be removed during garbage collection.
	// The Resource given is identified later on by its own Identity and a collection of a set of reserved identity values
	// that can have a special meaning.
	AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (newRes *descriptor.Resource, err error)

	// GetLocalResource retrieves a local resource from the repository.
	// The identity must match a resource in the component descriptor.
	GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (LocalBlob, error)
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
	// oci.NewFromTar, which interprets the blob as an OCILayout.
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
	// Each component version can resolve to a different store.
	StoreForReference(ctx context.Context, reference string) (Store, error)

	// ComponentVersionReference returns a unique reference for a component version.
	ComponentVersionReference(component, version string) string
}

// Store defines the interface for interacting with an OCI store.
type Store interface {
	content.ReadOnlyStorage
	content.Pusher
	content.TagResolver
	content.Tagger
}

// Repository implements the ComponentVersionRepository interface using OCI registries.
// Each component version is stored in a separate OCI repository.
type Repository struct {
	scheme *runtime.Scheme

	// localLayerBlobMemory temporarily stores local blobs until they are added to a component version.
	// thus any local blob added with AddLocalResource will be added to the memory until
	// AddComponentVersion is called with a reference to that resource.
	// Note that Store implementations are expected to either allow orphaned LocalBlobs or
	// regularly issue an async garbage collection to remove them.
	localLayerBlobMemory    LocalBlobMemory
	localManifestBlobMemory LocalBlobMemory

	// resolver resolves component version references to OCI stores.
	resolver Resolver

	// creatorAnnotation is the annotation used to identify the creator of the component version.
	// see AnnotationOCMCreator for more information.
	creatorAnnotation string

	// ResourceCopyOptions are the options used for copying resources between stores.
	// These options are used in copyResource.
	resourceCopyOptions oras.CopyOptions

	// localResourceCreationMode determines how resources should be accessed in the repository.
	localResourceCreationMode LocalResourceLayerCreationMode
}

var _ ComponentVersionRepository = (*Repository)(nil)

// AddComponentVersion adds a new component version to the repository.
func (repo *Repository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) (err error) {
	component, version := descriptor.Component.Name, descriptor.Component.Version
	done := logOperation(ctx, "add component version", slog.String("component", component), slog.String("version", version))
	defer done(err)

	reference := repo.resolver.ComponentVersionReference(component, version)
	store, err := repo.resolver.StoreForReference(ctx, reference)
	if err != nil {
		return fmt.Errorf("failed to resolve store for reference: %w", err)
	}

	manifest, err := addDescriptorToStore(ctx, store, descriptor, storeDescriptorOptions{
		Scheme:                        repo.scheme,
		CreatorAnnotation:             repo.creatorAnnotation,
		AdditionalDescriptorLayers:    repo.localLayerBlobMemory.GetBlobs(reference),
		AdditionalDescriptorManifests: repo.localManifestBlobMemory.GetBlobs(reference),
	})
	if err != nil {
		return fmt.Errorf("failed to add descriptor to store: %w", err)
	}

	// Tag the manifest with the reference
	if err := store.Tag(ctx, *manifest, reference); err != nil {
		return fmt.Errorf("failed to tag manifest: %w", err)
	}
	// Cleanup local blob memory as all layers have been pushed
	repo.localLayerBlobMemory.DeleteBlobs(reference)
	repo.localManifestBlobMemory.DeleteBlobs(reference)

	return nil
}

// GetComponentVersion retrieves a component version from the repository.
func (repo *Repository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	reference := repo.resolver.ComponentVersionReference(component, version)
	store, err := repo.resolver.StoreForReference(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("failed to get store for reference: %w", err)
	}

	desc, _, _, err := getDescriptorFromStore(ctx, store, reference)
	return desc, err
}

// AddLocalResource adds a local resource to the repository.
func (repo *Repository) AddLocalResource(
	ctx context.Context,
	component, version string,
	resource *descriptor.Resource,
	content blob.ReadOnlyBlob,
) (_ *descriptor.Resource, err error) {
	done := logOperation(ctx, "add local resource",
		slog.String("component", component),
		slog.String("version", version),
		slog.String("resource", resource.Name))
	defer done(err)

	reference := repo.resolver.ComponentVersionReference(component, version)
	store, err := repo.resolver.StoreForReference(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("failed to get store for reference: %w", err)
	}

	contentSizeAware, ok := content.(blob.SizeAware)
	if !ok {
		return nil, errors.New("content does not implement blob.SizeAware interface, size cannot be inferred")
	}

	size := contentSizeAware.Size()
	if size == blob.SizeUnknown {
		return nil, errors.New("content size is unknown")
	}

	if resource.Access.GetType().IsEmpty() {
		return nil, fmt.Errorf("resource access type is empty")
	}
	typed, err := repo.scheme.NewObject(resource.Access.GetType())
	if err != nil {
		return nil, fmt.Errorf("error creating resource access: %w", err)
	}
	if err := repo.scheme.Convert(resource.Access, typed); err != nil {
		return nil, fmt.Errorf("error converting resource access: %w", err)
	}

	switch typed := typed.(type) {
	case *v2.LocalBlob:
		switch typed.MediaType {
		// for local blobs that are complete image layouts, we can directly push them as part of the
		// descriptor index
		case MediaTypeOCIImageLayoutV1 + "+tar", MediaTypeOCIImageLayoutV1 + "+tar+gzip":
			ociStore, err := tar.ReadOCILayout(ctx, content)
			if err != nil {
				return nil, fmt.Errorf("failed to read OCI layout: %w", err)
			}
			defer func() {
				err = errors.Join(err, ociStore.Close())
			}()

			var group errgroup.Group
			for _, manifest := range ociStore.Index.Manifests {
				group.Go(func() error {
					if err := oras.CopyGraph(ctx, ociStore, store, manifest, repo.resourceCopyOptions.CopyGraphOptions); err != nil {
						return fmt.Errorf("failed to copy graph for manifest %v: %w", manifest, err)
					}
					return nil
				})
			}
			if err := group.Wait(); err != nil {
				return nil, fmt.Errorf("failed to copy oci layout: %w", err)
			}

			idxJSON, err := json.Marshal(ociStore.Index)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal index: %w", err)
			}
			layer := ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageIndex,
				Digest:    digest.FromBytes(idxJSON),
				Size:      int64(len(idxJSON)),
			}
			if err := adoptDescriptorBasedOnResource(&layer, resource); err != nil {
				return nil, fmt.Errorf("failed to adopt descriptor based on resource: %w", err)
			}
			if err := store.Push(ctx, layer, bytes.NewReader(idxJSON)); err != nil {
				return nil, fmt.Errorf("failed to push layer: %w", err)
			}
			repo.localManifestBlobMemory.AddBlob(reference, layer)

		default:
			// TODO(jakobmoellerdev): currently, by default all local blobs with undefined media types as well as
			//  ociImageSpecV1.MediaTypeImageLayer, ociImageSpecV1.MediaTypeImageLayerGzip are treated as image layers
			//  this is consistent with OCM v1.
			//  alternatively, we could store them as single Layer OCI Artifacts.
			layer := ociImageSpecV1.Descriptor{
				MediaType: typed.MediaType,
				Digest:    digest.Digest(typed.LocalReference),
				Size:      size,
			}
			if err := adoptDescriptorBasedOnResource(&layer, resource); err != nil {
				return nil, fmt.Errorf("failed to adopt descriptor based on resource: %w", err)
			}

			layerData, err := content.ReadCloser()
			if err != nil {
				return nil, fmt.Errorf("failed to get content reader: %w", err)
			}
			defer func() {
				err = errors.Join(err, layerData.Close())
			}()

			if err := store.Push(ctx, layer, io.NopCloser(layerData)); err != nil {
				return nil, fmt.Errorf("failed to push layer: %w", err)
			}

			repo.localLayerBlobMemory.AddBlob(reference, layer)
			if err := updateResourceAccessWithLayer(repo.scheme, resource, layer, reference, repo.localResourceCreationMode); err != nil {
				return nil, fmt.Errorf("failed to update resource access: %w", err)
			}
		}
	default:
		return nil, fmt.Errorf("unsupported resource access type: %T", typed)
	}

	return resource, nil
}

// GetLocalResource retrieves a local resource from the repository.
func (repo *Repository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (LocalBlob, error) {
	var err error
	done := logOperation(ctx, "get local resource",
		slog.String("component", component),
		slog.String("version", version),
		slog.Any("identity", identity))
	defer done(err)

	reference := repo.resolver.ComponentVersionReference(component, version)
	store, err := repo.resolver.StoreForReference(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("failed to get store for reference: %w", err)
	}

	desc, manifest, index, err := getDescriptorFromStore(ctx, store, reference)
	if err != nil {
		return nil, fmt.Errorf("failed to get component version: %w", err)
	}

	var candidates []descriptor.Resource
	for _, res := range desc.Component.Resources {
		if res.ElementMeta.ToIdentity().Match(identity, ídentitySubset) {
			candidates = append(candidates, res)
		}
	}
	if len(candidates) != 1 {
		return nil, fmt.Errorf("found %d candidates while looking for resource %v, but expected exactly one", len(candidates), identity)
	}

	resource := candidates[0]

	if resource.Access.GetType().IsEmpty() {
		return nil, fmt.Errorf("resource access type is empty")
	}
	typed, err := repo.scheme.NewObject(resource.Access.GetType())
	if err != nil {
		return nil, fmt.Errorf("error creating resource access: %w", err)
	}
	if err := repo.scheme.Convert(resource.Access, typed); err != nil {
		return nil, fmt.Errorf("error converting resource access: %w", err)
	}

	switch typed := typed.(type) {
	case *v2.LocalBlob:
		if index == nil {
			// if the index does not exist, we can only use the manifest
			// and thus local blobs can only be available as image layers
			return getLocalBlob(ctx, manifest, identity, store)
		}

		// if the index exists, we can use it to find certain media types that are compatible with
		// oci repositories.
		switch typed.MediaType {
		// for local blobs that are complete image layouts, we can directly push them as part of the
		// descriptor index
		case MediaTypeOCIImageLayoutV1 + "+tar", MediaTypeOCIImageLayoutV1 + "+tar+gzip":
			manifest, err := findMatchingDescriptor(index.Manifests, identity)
			if err != nil {
				return nil, fmt.Errorf("failed to find matching layer: %w", err)
			}
			return repo.ociLayoutFromStoreForDescriptor(ctx, &resource, store, manifest)
		// for anything else we cannot really do anything other than use a local blob
		default:
			return getLocalBlob(ctx, manifest, identity, store)
		}
	default:
		return nil, fmt.Errorf("unsupported resource access type: %T", typed)
	}
}

func getLocalBlob(ctx context.Context, manifest *ociImageSpecV1.Manifest, identity map[string]string, store Store) (LocalBlob, error) {
	layer, err := findMatchingDescriptor(manifest.Layers, identity)
	if err != nil {
		return nil, fmt.Errorf("failed to find matching layer: %w", err)
	}
	data, err := store.Fetch(ctx, layer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch layer data: %w", err)
	}
	if data == nil {
		return nil, fmt.Errorf("received nil data for layer %s", layer.Digest)
	}
	return NewDescriptorBlob(data, layer), nil
}

// UploadResource uploads a resource to the repository.
func (repo *Repository) UploadResource(ctx context.Context, target runtime.Typed, res *descriptor.Resource, b blob.ReadOnlyBlob) (err error) {
	done := logOperation(ctx, "upload resource", slog.String("resource", res.Name))
	defer done(err)

	var old v1.OCIImage
	if err := repo.scheme.Convert(res.Access, &old); err != nil {
		return fmt.Errorf("error converting resource old to OCI image: %w", err)
	}

	var access v1.OCIImage
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
		srcRef = parsedSrcRef.Reference
	}

	desc, err := oras.Copy(ctx, ociStore, srcRef, store, access.ImageReference, repo.resourceCopyOptions)

	res.Size = desc.Size
	// TODO(jakobmoellerdev): This might not be ideal because this digest
	//  is not representative of the entire OCI Layout, only of the descriptor.
	//  Eventually we should think about switching this to a genericBlobDigest.
	if err := ociDigestV1.ApplyToResource(res, desc.Digest); err != nil {
		return fmt.Errorf("failed to apply digest to resource: %w", err)
	}
	res.Access = &access

	return nil
}

// DownloadResource downloads a resource from the repository.
func (repo *Repository) DownloadResource(ctx context.Context, res *descriptor.Resource) (data blob.ReadOnlyBlob, err error) {
	done := logOperation(ctx, "download resource", slog.String("resource", res.Name))
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
		layer := v1.OCIImageLayer{}
		if err := repo.scheme.Convert(typed.GlobalAccess, &layer); err != nil {
			return nil, fmt.Errorf("error converting global blob access: %w", err)
		}
		return repo.getOCIImageLayer(ctx, &layer)
	case *v1.OCIImageLayer:
		return repo.getOCIImageLayer(ctx, typed)
	case *v1.OCIImage:
		src, err := repo.resolver.StoreForReference(ctx, typed.ImageReference)
		if err != nil {
			return nil, err
		}

		resolve, err := src.Resolve(ctx, typed.ImageReference)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve reference: %w", err)
		}

		return repo.ociLayoutFromStoreForDescriptor(ctx, res, src, resolve, typed.ImageReference)
	default:
		return nil, fmt.Errorf("unsupported resource access type: %T", typed)
	}
}

// getOCIImageLayerRecursively retrieves an OCIImageLayer from the repository.
// It resolves the reference to the OCI store and fetches the layer data.
// It returns a ReadOnlyBlob containing the layer data.
// It is able to handle both OCI image manifests and OCI image indexes as parent manifests.
// If its parent is an index, it will recursively search for the layer in the index.
func (repo *Repository) getOCIImageLayer(ctx context.Context, layer *v1.OCIImageLayer) (blob.ReadOnlyBlob, error) {
	src, err := repo.resolver.StoreForReference(ctx, layer.Reference)
	if err != nil {
		return nil, err
	}

	desc, err := src.Resolve(ctx, layer.Reference)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve reference: %w", err)
	}

	resolved, err := getOCIImageLayerRecursively(ctx, src, desc, layer)
	if err != nil {
		return nil, fmt.Errorf("failed to get OCI image layer: %w", err)
	}

	layerData, err := src.Fetch(ctx, resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch layer data: %w", err)
	}
	return NewDescriptorBlob(layerData, resolved), nil
}

// ociLayoutFromStoreForDescriptor creates an OCI layout from a store for a given descriptor.
func (repo *Repository) ociLayoutFromStoreForDescriptor(ctx context.Context, res *descriptor.Resource, src Store, desc ociImageSpecV1.Descriptor, tags ...string) (_ LocalBlob, err error) {
	mediaType := MediaTypeOCIImageLayoutV1 + "+tar" + "+gzip"
	var buf bytes.Buffer

	h := sha256.New()
	writer := io.MultiWriter(&buf, h)

	zippedBuf := gzip.NewWriter(writer)
	defer func() {
		if err != nil {
			// Clean up resources if there was an error
			zippedBuf.Close()
			buf.Reset()
		}
	}()

	target := tar.NewOCILayoutWriter(zippedBuf)
	defer func() {
		if terr := target.Close(); terr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close tar writer: %w", terr))
			return
		}
		if zerr := zippedBuf.Close(); zerr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close gzip writer: %w", zerr))
			return
		}
	}()

	if err := oras.CopyGraph(ctx, src, target, desc, repo.resourceCopyOptions.CopyGraphOptions); err != nil {
		return nil, fmt.Errorf("failed to copy graph for descriptor %v: %w", desc, err)
	}

	for _, tag := range tags {
		if err := target.Tag(ctx, desc, tag); err != nil {
			return nil, fmt.Errorf("failed to tag manifest: %w", err)
		}
	}

	// now close prematurely so that the buf is fully filled before we set things like size and digest.
	if err := errors.Join(target.Close(), zippedBuf.Close()); err != nil {
		return nil, fmt.Errorf("failed to close writers: %w", err)
	}

	downloaded := blob.NewDirectReadOnlyBlob(&buf)

	downloaded.SetPrecalculatedSize(int64(buf.Len()))
	downloaded.SetMediaType(mediaType)

	blobDigest := digest.NewDigest(digest.SHA256, h)
	downloaded.SetPrecalculatedDigest(blobDigest.String())

	// Validate the digest of the downloaded content matches what we expect
	if err := validateDigest(res, desc, blobDigest); err != nil {
		return nil, fmt.Errorf("digest validation failed: %w", err)
	}

	return NewResourceBlob(res, downloaded, mediaType), nil
}

func validateDigest(res *descriptor.Resource, desc ociImageSpecV1.Descriptor, blobDigest digest.Digest) error {
	if res.Digest == nil {
		// the resource does not have a digest, so we cannot validate it
		return nil
	}

	expected := digest.NewDigestFromEncoded(ociDigestV1.SHAMapping[res.Digest.HashAlgorithm], res.Digest.Value)

	var actual digest.Digest
	switch res.Digest.NormalisationAlgorithm {
	case ociDigestV1.OCIArtifactDigestAlgorithm:
		// the digest is based on the leading descriptor
		actual = desc.Digest
	// TODO(jakobmoellerdev): we need to switch to a blob package digest eventually
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
func getDescriptorOCIImageManifest(ctx context.Context, store Store, reference string) (manifest ociImageSpecV1.Manifest, index *ociImageSpecV1.Index, err error) {
	logger.Log(ctx, slog.LevelInfo, "resolving descriptor", slog.String("reference", reference))
	base, err := store.Resolve(ctx, reference)
	if err != nil {
		return ociImageSpecV1.Manifest{}, nil, fmt.Errorf("failed to resolve reference %q: %w", reference, err)
	}
	logger.Log(ctx, slog.LevelInfo, "fetching descriptor", descriptorLogAttr(base))
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

// updateResourceAccessWithLayer updates the resource access with the new layer information.
// for setting a global access it uses the base reference given which must not already contain a digest.
func updateResourceAccessWithLayer(scheme *runtime.Scheme, resource *descriptor.Resource, layer ociImageSpecV1.Descriptor, base string, mode LocalResourceLayerCreationMode) error {
	if resource == nil {
		return errors.New("resource must not be nil")
	}

	// Create OCI image layer access
	access := &v1.OCIImageLayer{
		Reference: base,
		Digest:    layer.Digest,
		MediaType: layer.MediaType,
		Size:      layer.Size,
	}

	// Create access based on configured mode
	switch mode {
	case LocalResourceCreationModeOCIImageLayer:
		resource.Access = access
	case LocalResourceCreationModeLocalBlobWithNestedGlobalAccess:
		// Create local blob access
		access, err := descriptor.ConvertToV2LocalBlob(scheme, &descriptor.LocalBlob{
			Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			LocalReference: layer.Digest.String(),
			MediaType:      layer.MediaType,
			GlobalAccess:   access,
		})
		if err != nil {
			return fmt.Errorf("failed to convert access to local blob: %w", err)
		}
		resource.Access = access
	default:
		return fmt.Errorf("unsupported access mode: %s", mode)
	}

	if err := ociDigestV1.ApplyToResource(resource, layer.Digest); err != nil {
		return fmt.Errorf("failed to apply digest to resource: %w", err)
	}

	return nil
}

// findMatchingDescriptor finds a layer in the manifest that matches the given identity.
func findMatchingDescriptor(manifests []ociImageSpecV1.Descriptor, identity runtime.Identity) (ociImageSpecV1.Descriptor, error) {
	var notMatched []ociImageSpecV1.Descriptor

	for _, layer := range manifests {
		artifactAnnotations, err := GetArtifactOCILayerAnnotations(&layer)
		if errors.Is(err, ErrArtifactOCILayerAnnotationDoesNotExist) || len(artifactAnnotations) == 0 {
			notMatched = append(notMatched, layer)
			continue
		}
		if err != nil {
			return ociImageSpecV1.Descriptor{}, fmt.Errorf("error getting artifact annotation: %w", err)
		}

		for _, artifactAnnotation := range artifactAnnotations {
			if artifactAnnotation.Kind != ArtifactKindResource {
				notMatched = append(notMatched, layer)
				continue
			}
			if identity.Match(artifactAnnotation.Identity) {
				return layer, nil
			}
		}
		notMatched = append(notMatched, layer)
	}

	return ociImageSpecV1.Descriptor{}, fmt.Errorf("no matching descriptor for identity %v (not matched other descriptors %v): %w", identity, notMatched, errdef.ErrNotFound)
}

// TODO: Contribute to identity runtime.
var ídentitySubset = runtime.IdentityMatchingChainFn(func(a runtime.Identity, b runtime.Identity) bool {
	return isMapSubset(a, b)
})

func isMapSubset[K, V comparable](m, sub map[K]V) bool {
	if len(sub) > len(m) {
		return false
	}
	for k, vsub := range sub {
		if vm, found := m[k]; !found || vm != vsub {
			return false
		}
	}
	return true
}
