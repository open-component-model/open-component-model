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
	"os"
	"path/filepath"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
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

	MediaTypeOCIImageLayout   = "application/vnd.ocm.software.oci.layout"
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
	GetLocalResource(ctx context.Context, component, version string, identity map[string]string) (LocalBlob, error)
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

	// localBlobMemory temporarily stores local blobs until they are added to a component version.
	// thus any local blob added with AddLocalResource will be added to the memory until
	// AddComponentVersion is called with a reference to that resource.
	// Note that Store implementations are expected to either allow orphaned LocalBlobs or
	// regularly issue an async garbage collection to remove them.
	localBlobMemory LocalBlobMemory

	// resolver resolves component version references to OCI stores.
	resolver Resolver

	// creatorAnnotation is the annotation used to identify the creator of the component version.
	// see AnnotationOCMCreator for more information.
	creatorAnnotation string

	// ResourceCopyOptions are the options used for copying resources between stores.
	// These options are used in copyResource.
	resourceCopyOptions oras.CopyOptions
}

var _ ComponentVersionRepository = (*Repository)(nil)

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
		return nil, err
	}

	access, err := getLocalBlobAccess(repo.scheme, resource)
	if err != nil {
		return nil, err
	}

	contentSizeAware, ok := content.(blob.SizeAware)
	if !ok {
		return nil, errors.New("content does not implement blob.SizeAware interface, size cannot be inferred")
	}

	size := contentSizeAware.Size()
	if size == blob.SizeUnknown {
		return nil, errors.New("content size is unknown")
	}

	layer, err := layerFromResourceIdentityAndLocalBlob(access, size, resource)
	if err != nil {
		return nil, fmt.Errorf("error creating layer descriptor: %w", err)
	}

	layerData, err := content.ReadCloser()
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, layerData.Close())
	}()

	if err := store.Push(ctx, layer, io.NopCloser(layerData)); err != nil {
		return nil, err
	}

	repo.localBlobMemory.AddBlob(reference, layer)
	if err := updateResourceAccess(resource, layer); err != nil {
		return nil, err
	}

	return resource, nil
}

// GetLocalResource retrieves a local resource from the repository.
func (repo *Repository) GetLocalResource(ctx context.Context, component, version string, identity map[string]string) (LocalBlob, error) {
	var err error
	done := logOperation(ctx, "get local resource",
		slog.String("component", component),
		slog.String("version", version),
		slog.Any("identity", identity))
	defer done(err)

	if component == "" || version == "" {
		return nil, errors.New("component and version must not be empty")
	}
	if len(identity) == 0 {
		return nil, errors.New("identity must not be empty")
	}

	reference := repo.resolver.ComponentVersionReference(component, version)
	store, err := repo.resolver.StoreForReference(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("failed to get store for reference: %w", err)
	}

	manifest, err := getOCIImageManifest(ctx, store, reference)
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest: %w", err)
	}

	layer, err := findMatchingLayer(manifest, identity)
	if err != nil {
		return nil, err
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

	// Encode and upload the descriptor
	descriptorEncoding, descriptorBuffer, err := tar.SingleFileTAREncodeV2Descriptor(repo.scheme, descriptor)
	if err != nil {
		return fmt.Errorf("failed to encode descriptor: %w", err)
	}
	descriptorBytes := descriptorBuffer.Bytes()
	descriptorOCIDescriptor := ociImageSpecV1.Descriptor{
		MediaType: MediaTypeComponentDescriptorV2 + descriptorEncoding,
		Digest:    digest.FromBytes(descriptorBytes),
		Size:      int64(len(descriptorBytes)),
	}
	logger.Log(ctx, slog.LevelDebug, "pushing descriptor", descriptorLogAttr(descriptorOCIDescriptor))
	if err := store.Push(ctx, descriptorOCIDescriptor, bytes.NewReader(descriptorBytes)); err != nil {
		return fmt.Errorf("unable to push component descriptor: %w", err)
	}

	// Create and upload the component configuration
	componentConfigRaw, componentConfigDescriptor, err := createComponentConfig(descriptorOCIDescriptor)
	if err != nil {
		return fmt.Errorf("failed to marshal component config: %w", err)
	}
	logger.Log(ctx, slog.LevelDebug, "pushing descriptor", descriptorLogAttr(componentConfigDescriptor))
	if err := store.Push(ctx, componentConfigDescriptor, bytes.NewReader(componentConfigRaw)); err != nil {
		return fmt.Errorf("unable to push component config: %w", err)
	}

	// Create and upload the manifest
	manifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    componentConfigDescriptor,
		Annotations: map[string]string{
			AnnotationOCMComponentVersion: fmt.Sprintf("component-descriptors/%s:%s", component, version),
			AnnotationOCMCreator:          repo.creatorAnnotation,
		},
		Layers: append(
			[]ociImageSpecV1.Descriptor{descriptorOCIDescriptor},
			repo.localBlobMemory.GetBlobs(reference)...,
		),
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	manifestDescriptor := ociImageSpecV1.Descriptor{
		MediaType:   manifest.MediaType,
		Digest:      digest.FromBytes(manifestRaw),
		Size:        int64(len(manifestRaw)),
		Annotations: manifest.Annotations,
	}
	logger.Log(ctx, slog.LevelInfo, "pushing descriptor", descriptorLogAttr(manifestDescriptor))
	if err := store.Push(ctx, manifestDescriptor, bytes.NewReader(manifestRaw)); err != nil {
		return fmt.Errorf("unable to push manifest: %w", err)
	}

	// Tag the manifest with the reference
	if err := store.Tag(ctx, manifestDescriptor, reference); err != nil {
		return fmt.Errorf("failed to tag manifest: %w", err)
	}

	// Cleanup local blob memory as all layers have been pushed
	repo.localBlobMemory.DeleteBlobs(reference)

	return nil
}

// GetComponentVersion retrieves a component version from the repository.
func (repo *Repository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	reference := repo.resolver.ComponentVersionReference(component, version)
	store, err := repo.resolver.StoreForReference(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("failed to get store for reference: %w", err)
	}

	manifest, err := getOCIImageManifest(ctx, store, reference)
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest: %w", err)
	}

	componentConfigRaw, err := store.Fetch(ctx, manifest.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to get component config: %w", err)
	}
	defer func() {
		_ = componentConfigRaw.Close()
	}()
	componentConfig := ComponentConfig{}
	if err := json.NewDecoder(componentConfigRaw).Decode(&componentConfig); err != nil {
		return nil, err
	}

	// Read component descriptor
	descriptorRaw, err := store.Fetch(ctx, *componentConfig.ComponentDescriptorLayer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch descriptor layer: %w", err)
	}
	defer func() {
		_ = descriptorRaw.Close()
	}()

	return tar.SingleFileTARDecodeV2Descriptor(descriptorRaw)
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

	fileBufferPath, err := prepOCILayoutFileBuffer(res, b)
	if err != nil {
		return err
	}

	// If src is a string, it's a tar file path
	ociStore, err := oci.NewFromTar(ctx, fileBufferPath)
	if err != nil {
		return err
	}

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
		return errors.Join(os.Remove(fileBufferPath), err)
	}
	defer os.Remove(fileBufferPath)

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

	var access v1.OCIImage
	if err := repo.scheme.Convert(res.Access, &access); err != nil {
		return nil, fmt.Errorf("error converting resource access to OCI image: %w", err)
	}
	src, err := repo.resolver.StoreForReference(ctx, access.ImageReference)
	if err != nil {
		return nil, err
	}

	// any resource that gets downloaded is made available as a gzipped tar'ed OCI Layout archive.
	mediaType := MediaTypeOCIImageLayoutV1 + "+tar" + "+gzip"

	// TODO(jakobmoellerdev) we might need to determine if we download to a buffer or a file
	//  based on the size of the blob. But that should be done with better settings and maybe
	//  some helpers from the blob package.
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

	// for download we ignore the descriptor root that is returned from the copy aside from using it for
	// download verification
	desc, err := oras.Copy(ctx, src, access.ImageReference, target, access.ImageReference, repo.resourceCopyOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to copy resource: %w", err)
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

	if err := validateDigest(res, desc, blobDigest); err != nil {
		return nil, fmt.Errorf("failed to validate digest after download of resource: %w", err)
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

// getOCIImageManifest retrieves the manifest for a given reference from the store.
func getOCIImageManifest(ctx context.Context, store Store, reference string) (manifest ociImageSpecV1.Manifest, err error) {
	manifestDigest, err := store.Resolve(ctx, reference)
	if err != nil {
		return ociImageSpecV1.Manifest{}, fmt.Errorf("failed to resolve reference %q: %w", reference, err)
	}
	logger.Log(ctx, slog.LevelInfo, "fetching descriptor", descriptorLogAttr(manifestDigest))
	manifestRaw, err := store.Fetch(ctx, ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    manifestDigest.Digest,
		Size:      manifestDigest.Size,
	})
	if err != nil {
		return ociImageSpecV1.Manifest{}, err
	}
	defer func() {
		err = errors.Join(err, manifestRaw.Close())
	}()
	if err := json.NewDecoder(manifestRaw).Decode(&manifest); err != nil {
		return ociImageSpecV1.Manifest{}, err
	}
	return manifest, nil
}

// updateResourceAccess updates the resource access with the new layer information.
func updateResourceAccess(resource *descriptor.Resource, layer ociImageSpecV1.Descriptor) error {
	if resource == nil {
		return errors.New("resource must not be nil")
	}

	resource.Access = &descriptor.LocalBlob{
		LocalReference: layer.Digest.String(),
		MediaType:      layer.MediaType,
		GlobalAccess: &v1.OCIImageLayer{
			Digest:    layer.Digest,
			MediaType: layer.MediaType,
			Reference: fmt.Sprintf("%s@%s", layer.Digest.String(), layer.Digest.String()),
			Size:      layer.Size,
		},
	}

	if err := ociDigestV1.ApplyToResource(resource, layer.Digest); err != nil {
		return fmt.Errorf("failed to apply digest to resource: %w", err)
	}

	return nil
}

// prepOCILayoutFileBuffer creates a temporary file with the resource content that is expected
// to be an OCI Layout. It handles gzip detection and writes the content to the file.
func prepOCILayoutFileBuffer(resource *descriptor.Resource, content blob.ReadOnlyBlob) (path string, err error) {
	filePath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d", resource.Name, time.Now().UnixNano()))
	tmpFile, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tmpFile.Close(), os.Remove(filePath))
		}
	}()

	var reader io.ReadCloser
	if reader, err = content.ReadCloser(); err != nil {
		return "", fmt.Errorf("failed to get resource content: %w", err)
	}
	defer func() {
		err = errors.Join(err, reader.Close())
	}()

	if err = copyWithGzipDetection(reader, tmpFile); err != nil {
		return "", fmt.Errorf("failed to copy resource content: %w", err)
	}

	if err = tmpFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temporary file: %w", err)
	}

	return filePath, nil
}

func copyWithGzipDetection(src io.Reader, dst io.Writer) (err error) {
	const gzipMagic1, gzipMagic2 = 0x1F, 0x8B
	var header [2]byte

	// Read the first two bytes for gzip detection
	n, err := io.ReadFull(src, header[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("failed to read data for gzip detection: %w", err)
	}

	// Reconstruct reader with the first two bytes prepended
	reader := io.MultiReader(bytes.NewReader(header[:n]), src)

	if n == 2 && header[0] == gzipMagic1 && header[1] == gzipMagic2 {
		var gzReader *gzip.Reader
		if gzReader, err = gzip.NewReader(reader); err != nil {
			return fmt.Errorf("failed to initialize gzip reader: %w", err)
		}
		defer func() {
			err = errors.Join(err, gzReader.Close())
		}()
		reader = gzReader
	}

	if _, err = io.Copy(dst, reader); err != nil {
		return fmt.Errorf("failed to write content to temporary file: %w", err)
	}

	return nil
}

// findMatchingLayer finds a layer in the manifest that matches the given identity.
func findMatchingLayer(manifest ociImageSpecV1.Manifest, identity runtime.Identity) (ociImageSpecV1.Descriptor, error) {
	var notMatched []ociImageSpecV1.Descriptor

	for _, layer := range manifest.Layers {
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

	return ociImageSpecV1.Descriptor{}, fmt.Errorf("no matching layers for identity %v (not matched other layers %v): %w", identity, notMatched, errdef.ErrNotFound)
}

// getLocalBlobAccess validates and converts a resource's access information to v2.LocalBLob format.
func getLocalBlobAccess(scheme *runtime.Scheme, resource *descriptor.Resource) (*v2.LocalBlob, error) {
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required for uploading to an OCI repository")
	}

	var access v2.LocalBlob
	if err := scheme.Convert(resource.Access, &access); err != nil {
		return nil, fmt.Errorf("error converting resource access to OCI image: %w", err)
	}

	if access.MediaType == "" {
		return nil, fmt.Errorf("resource access media type is required for uploading to an OCI repository")
	}

	layerDigest := digest.Digest(access.LocalReference)
	if err := layerDigest.Validate(); err != nil {
		return nil, fmt.Errorf("invalid layer digest in local reference: %w", err)
	}

	return &access, nil
}
