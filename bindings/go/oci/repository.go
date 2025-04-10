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
	"strings"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	"ocm.software/open-component-model/bindings/go/oci/internal/identity"
	"ocm.software/open-component-model/bindings/go/oci/internal/lister"
	complister "ocm.software/open-component-model/bindings/go/oci/internal/lister/component"
	"ocm.software/open-component-model/bindings/go/oci/internal/log"
	"ocm.software/open-component-model/bindings/go/oci/internal/memory"
	"ocm.software/open-component-model/bindings/go/oci/internal/singlelayer"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	descriptor2 "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	digestv1 "ocm.software/open-component-model/bindings/go/oci/spec/digest/v1"
	indexv1 "ocm.software/open-component-model/bindings/go/oci/spec/index/component/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

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
// Each component version is stored in a separate OCI repository.
//
// This Repository implementation synchronizes OCI Manifests through the concepts of LocalDescriptorMemory.
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

	// localManifestMemory temporarily stores local blobs intended as manifests until they are added to a component version.
	localManifestMemory memory.LocalDescriptorMemory

	// resolver resolves component version references to OCI stores.
	resolver Resolver

	// creatorAnnotation is the annotation used to identify the creator of the component version.
	// see OCMCreator for more information.
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
	done := log.Operation(ctx, "add component version", slog.String("component", component), slog.String("version", version))
	defer done(err)

	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return err
	}

	manifest, err := addDescriptorToStore(ctx, store, descriptor, storeDescriptorOptions{
		Scheme:                        repo.scheme,
		Author:                        repo.creatorAnnotation,
		AdditionalDescriptorManifests: repo.localManifestMemory.Get(reference),
	})
	if err != nil {
		return fmt.Errorf("failed to add descriptor to store: %w", err)
	}

	// Tag the manifest with the reference
	if err := store.Tag(ctx, *manifest, reference); err != nil {
		return fmt.Errorf("failed to tag manifest: %w", err)
	}
	// Cleanup local blob memory as all layers have been pushed
	repo.localManifestMemory.Delete(reference)

	return nil
}

func (repo *Repository) ListComponentVersions(ctx context.Context, component string) (_ []string, err error) {
	done := log.Operation(ctx, "list component versions",
		slog.String("component", component))
	defer done(err)

	ref, store, err := repo.getStore(ctx, component, "latest")
	if err != nil {
		return nil, err
	}

	list, err := lister.New(store)
	if err != nil {
		return nil, fmt.Errorf("failed to create lister: %w", err)
	}

	tagResolver, err := complister.ReferenceTagVersionResolver(ref, store)
	if err != nil {
		return nil, fmt.Errorf("failed to create tag resolver: %w", err)
	}

	return list.List(ctx, lister.Options{
		SortPolicy:   lister.SortPolicyLooseSemverDescending,
		LookupPolicy: lister.LookupPolicyReferrerWithTagFallback,
		TagListerOptions: lister.TagListerOptions{
			VersionResolver: tagResolver,
		},
		ReferrerListerOptions: lister.ReferrerListerOptions{
			ArtifactType:    descriptor2.MediaTypeComponentDescriptorV2,
			Subject:         indexv1.Descriptor,
			VersionResolver: complister.ReferrerAnnotationVersionResolver(component),
		},
	})
}

// GetComponentVersion retrieves a component version from the repository.
func (repo *Repository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return nil, err
	}

	desc, _, _, err := getDescriptorFromStore(ctx, store, reference)
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
		slog.String("resource", resource.Name))
	defer done(err)

	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return nil, err
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
		case layout.MediaTypeOCIImageLayoutV1 + "+tar", layout.MediaTypeOCIImageLayoutV1 + "+tar+gzip":
			index, err := tar.CopyOCILayout(ctx, store, b, tar.CopyOCILayoutOptions{
				CopyGraphOptions: repo.resourceCopyOptions.CopyGraphOptions,
				MutateIndexFunc: func(idx *ociImageSpecV1.Descriptor) error {
					return identity.AdoptAsResource(idx, resource)
				},
			})
			if err != nil {
				return nil, fmt.Errorf("failed to copy OCI layout: %w", err)
			}
			if err := updateResourceAccessWithOCIDescriptor(repo.scheme, resource, index, reference, repo.localResourceCreationMode); err != nil {
				return nil, fmt.Errorf("failed to update resource access: %w", err)
			}

			repo.localManifestMemory.Add(reference, index)
		default:
			contentSizeAware, ok := b.(blob.SizeAware)
			if !ok {
				return nil, errors.New("blob does not implement blob.SizeAware interface, size cannot be inferred")
			}
			size := contentSizeAware.Size()
			if size == blob.SizeUnknown {
				return nil, errors.New("blob size is unknown and cannot be packed into a single layer artifact")
			}
			mediaType := typed.MediaType
			if mediaType == "" {
				if mediaTypeFromBlob, ok := b.(blob.MediaTypeAware); ok {
					if mediaTypeFromBlob, ok := mediaTypeFromBlob.MediaType(); ok {
						mediaType = mediaTypeFromBlob
					}
				}
			}

			layer := ociImageSpecV1.Descriptor{
				MediaType:   mediaType,
				Digest:      digest.Digest(typed.LocalReference),
				Size:        size,
				Annotations: map[string]string{},
			}
			if err := identity.AdoptAsResource(&layer, resource); err != nil {
				return nil, fmt.Errorf("failed to adopt descriptor based on resource: %w", err)
			}
			manifest, err := singlelayer.PackSingleLayerOCIArtifact(ctx, store, b, singlelayer.PackOptions{
				Main:         layer,
				ArtifactType: mediaType,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to pack single layer OCI artifact: %w", err)
			}

			if err := updateResourceAccessWithOCIDescriptor(repo.scheme, resource, manifest, reference, repo.localResourceCreationMode); err != nil {
				return nil, fmt.Errorf("failed to update resource access: %w", err)
			}

			repo.localManifestMemory.Add(reference, manifest)
		}
	default:
		return nil, fmt.Errorf("unsupported resource access type: %T", typed)
	}

	return resource, nil
}

// GetLocalResource retrieves a local resource from the repository.
func (repo *Repository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (LocalBlob, *descriptor.Resource, error) {
	var err error
	done := log.Operation(ctx, "get local resource",
		slog.String("component", component),
		slog.String("version", version),
		slog.Any("identity", identity))
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
		return nil, nil, fmt.Errorf("found %d candidates while looking for resource %v, but expected exactly one", len(candidates), identity)
	}
	resource := candidates[0]
	log.Base.Info("found resource in descriptor", "resource", resource.ToIdentity())

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
		if index == nil {
			// if the index does not exist, we can only use the manifest
			// and thus local blobs can only be available as image layers
			b, err := getLocalBlob(ctx, manifest, identity, store)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to get local blob: %w", err)
			}
			return b, &resource, nil
		}

		// if the index exists, we can use it to find certain media types that are compatible with
		// oci repositories.
		switch typed.MediaType {
		// for local blobs that are complete image layouts, we can directly push them as part of the
		// descriptor index
		case layout.MediaTypeOCIImageLayoutV1 + "+tar", layout.MediaTypeOCIImageLayoutV1 + "+tar+gzip", ociImageSpecV1.MediaTypeImageIndex:
			artifact, err := annotations.FilterFirstMatchingArtifact(index.Manifests, identity, annotations.ArtifactKindResource)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to find matching descriptor: %w", err)
			}
			b, err := repo.generateOCILayout(ctx, &resource, store, artifact)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to get local blob: %w", err)
			}
			return b, &resource, nil
		// for anything else we cannot really do anything other than use a local blob
		default:
			b, err := getSingleLayerManifestBlob(ctx, index, identity, store)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to get local blob: %w", err)
			}
			return b, &resource, nil
		}
	default:
		return nil, nil, fmt.Errorf("unsupported resource access type: %T", typed)
	}
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
	done := log.Operation(ctx, "upload resource", slog.String("resource", res.Name))
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
		srcRef = parsedSrcRef.Reference
	}

	desc, err := oras.Copy(ctx, ociStore, srcRef, store, access.ImageReference, repo.resourceCopyOptions)
	if err != nil {
		return fmt.Errorf("failed to upload resource via copy: %w", err)
	}

	res.Size = desc.Size
	// TODO(jakobmoellerdev): This might not be ideal because this digest
	//  is not representative of the entire OCI Layout, only of the descriptor.
	//  Eventually we should think about switching this to a genericBlobDigest.
	if err := digestv1.ApplyToResource(res, desc.Digest); err != nil {
		return fmt.Errorf("failed to apply digest to resource: %w", err)
	}
	res.Access = &access

	return nil
}

// DownloadResource downloads a resource from the repository.
func (repo *Repository) DownloadResource(ctx context.Context, res *descriptor.Resource) (data blob.ReadOnlyBlob, err error) {
	done := log.Operation(ctx, "download resource", slog.String("resource", res.Name))
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
		image := accessv1.OCIImage{}
		if err := repo.scheme.Convert(typed.GlobalAccess, &image); err != nil {
			return nil, fmt.Errorf("error converting global blob access: %w", err)
		}
		res := res.DeepCopy()
		res.Access = &image
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

		return repo.generateOCILayout(ctx, res, src, desc, typed.ImageReference)
	default:
		return nil, fmt.Errorf("unsupported resource access type: %T", typed)
	}
}

// generateOCILayout creates an OCI layout from a store for a given descriptor.
func (repo *Repository) generateOCILayout(ctx context.Context, res *descriptor.Resource, src spec.Store, desc ociImageSpecV1.Descriptor, tags ...string) (_ LocalBlob, err error) {
	mediaType := layout.MediaTypeOCIImageLayoutV1 + "+tar" + "+gzip"
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

	return ociblob.NewResourceBlob(res, downloaded, mediaType), nil
}

func validateDigest(res *descriptor.Resource, desc ociImageSpecV1.Descriptor, blobDigest digest.Digest) error {
	if res.Digest == nil {
		// the resource does not have a digest, so we cannot validate it
		return nil
	}

	expected := digest.NewDigestFromEncoded(digestv1.SHAMapping[res.Digest.HashAlgorithm], res.Digest.Value)

	var actual digest.Digest
	switch res.Digest.NormalisationAlgorithm {
	case digestv1.OCIArtifactDigestAlgorithm:
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

func getSingleLayerManifestBlob(ctx context.Context, index *ociImageSpecV1.Index, identity map[string]string, store oras.ReadOnlyTarget) (LocalBlob, error) {
	layer, err := annotations.FilterFirstMatchingArtifact(index.Manifests, identity, annotations.ArtifactKindResource)
	if err != nil {
		return nil, fmt.Errorf("failed to find matching layer: %w", err)
	}
	data, err := store.Fetch(ctx, layer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch layer data: %w", err)
	}
	defer func() {
		_ = data.Close()
	}()
	manifest := ociImageSpecV1.Manifest{}
	if err := json.NewDecoder(data).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %w", err)
	}
	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("manifest has no layers and cannot be used to get a local blob")
	}
	layer = manifest.Layers[0]
	layerData, err := store.Fetch(ctx, layer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch layer data: %w", err)
	}

	return ociblob.NewDescriptorBlob(layerData, layer), nil
}

func getLocalBlob(ctx context.Context, manifest *ociImageSpecV1.Manifest, identity map[string]string, store oras.ReadOnlyTarget) (LocalBlob, error) {
	layer, err := annotations.FilterFirstMatchingArtifact(manifest.Layers, identity, annotations.ArtifactKindResource)
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
	return ociblob.NewDescriptorBlob(data, layer), nil
}

// getDescriptorOCIImageManifest retrieves the manifest for a given reference from the store.
// It handles both OCI image indexes and OCI image manifests.
func getDescriptorOCIImageManifest(ctx context.Context, store spec.Store, reference string) (manifest ociImageSpecV1.Manifest, index *ociImageSpecV1.Index, err error) {
	log.Base.Log(ctx, slog.LevelInfo, "resolving descriptor", slog.String("reference", reference))
	base, err := store.Resolve(ctx, reference)
	if err != nil {
		return ociImageSpecV1.Manifest{}, nil, fmt.Errorf("failed to resolve reference %q: %w", reference, err)
	}
	log.Base.Log(ctx, slog.LevelInfo, "fetching descriptor", log.DescriptorLogAttr(base))
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

// updateResourceAccessWithOCIDescriptor updates the resource access with the new layer information.
// for setting a global access it uses the base reference given which must not already contain a digest.
func updateResourceAccessWithOCIDescriptor(scheme *runtime.Scheme, resource *descriptor.Resource, desc ociImageSpecV1.Descriptor, base string, mode LocalResourceLayerCreationMode) error {
	if resource == nil {
		return errors.New("resource must not be nil")
	}

	access := &accessv1.OCIImage{
		ImageReference: fmt.Sprintf("%s@%s", base, desc.Digest.String()),
	}

	// Create access based on configured mode
	switch mode {
	case LocalResourceCreationModeOCIImage:
		resource.Access = access
	case LocalResourceCreationModeLocalBlobWithNestedGlobalAccess:
		// Create local blob access
		access, err := descriptor.ConvertToV2LocalBlob(scheme, &descriptor.LocalBlob{
			Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			LocalReference: desc.Digest.String(),
			MediaType:      desc.MediaType,
			GlobalAccess:   access,
		})
		if err != nil {
			return fmt.Errorf("failed to convert access to local blob: %w", err)
		}
		resource.Access = access
	default:
		return fmt.Errorf("unsupported access mode: %s", mode)
	}

	if err := digestv1.ApplyToResource(resource, desc.Digest); err != nil {
		return fmt.Errorf("failed to apply digest to resource: %w", err)
	}

	return nil
}
