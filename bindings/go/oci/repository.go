// Package oci provides functionality for storing and retrieving Open Component Model (OCM) components
// using the Open Container Initiative (OCI) registry format. It implements the OCM repository interface
// using OCI registries as the underlying storage mechanism.

package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"sigs.k8s.io/yaml"

	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/errdef"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocmoci "ocm.software/open-component-model/bindings/go/oci/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/access/v1"
	ociDigestV1 "ocm.software/open-component-model/bindings/go/oci/digest/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Media type constants for component descriptors
const (
	// MediaTypeComponentDescriptor is the base media type for OCM component descriptors
	MediaTypeComponentDescriptor = "application/vnd.ocm.software/ocm.component-descriptor"
	// MediaTypeComponentDescriptorV2 is the media type for version 2 of OCM component descriptors
	MediaTypeComponentDescriptorV2 = MediaTypeComponentDescriptor + ".v2"
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

// OCMComponentVersionRepository defines the interface for storing and retrieving OCM component versions
// and their associated resources in an OCI repository.
type OCMComponentVersionRepository interface {
	// AddComponentVersion adds a new component version to the repository.
	// If a component version already exists, it will be updated with the new descriptor.
	AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error

	// GetComponentVersion retrieves a component version from the repository.
	// Returns the descriptor from the most recent AddComponentVersion call for that component and version.
	GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error)

	// AddLocalResource adds a local resource to the repository.
	// The resource must be referenced in the component descriptor.
	// Resources for non-existent component versions may be stored but may be removed during garbage collection.
	AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (newRes *descriptor.Resource, err error)

	// GetLocalResource retrieves a local resource from the repository.
	// The identity must match a resource in the component descriptor.
	GetLocalResource(ctx context.Context, component, version string, identity map[string]string) (LocalBlob, error)
}

// OCMResourceRepository defines the interface for storing and retrieving OCM resources
// independently of component versions.
type OCMResourceRepository interface {
	// UploadResource uploads a resource to the repository.
	// Returns the updated resource with repository-specific information.
	UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob) (newRes *descriptor.Resource, err error)

	// DownloadResource downloads a resource from the repository.
	// Returns a blob containing the resource data.
	DownloadResource(ctx context.Context, res *descriptor.Resource) (content blob.ReadOnlyBlob, err error)
}

// Resolver defines the interface for resolving references to OCI stores.
type Resolver interface {
	// StoreForReference resolves a reference to a Store.
	// Each component version can resolve to a different store.
	StoreForReference(ctx context.Context, reference string) (Store, error)

	// ComponentVersionReference returns a unique reference for a component version.
	ComponentVersionReference(component, version string) string

	// TargetResourceReference returns a reference for uploading a resource.
	TargetResourceReference(srcReference string) (targetReference string, err error)
}

// Store defines the interface for interacting with an OCI store.
type Store interface {
	content.ReadOnlyStorage
	content.Pusher
	content.TagResolver
	content.Tagger
}

// Repository implements the OCMComponentVersionRepository interface using OCI registries.
// Each component version is stored in a separate OCI repository.
type Repository struct {
	scheme *runtime.Scheme

	// localBlobMemory temporarily stores local blobs until they are added to a component version.
	localBlobMemory *LocalBlobMemory

	// resolver resolves component version references to OCI stores.
	resolver Resolver
}

// RepositoryFromResolverAndMemory creates a new Repository instance.
func RepositoryFromResolverAndMemory(resolver Resolver, blobMemory *LocalBlobMemory) *Repository {
	scheme := runtime.NewScheme()
	ocmoci.MustAddToScheme(scheme)
	v2.MustAddToScheme(scheme)
	return &Repository{
		resolver:        resolver,
		localBlobMemory: blobMemory,
		scheme:          scheme,
	}
}

var _ OCMComponentVersionRepository = (*Repository)(nil)

// AddLocalResource adds a local resource to the repository.
func (repo *Repository) AddLocalResource(
	ctx context.Context,
	component, version string,
	resource *descriptor.Resource,
	content blob.ReadOnlyBlob,
) (newRes *descriptor.Resource, err error) {
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
		return nil, fmt.Errorf("content does not implement blob.SizeAware interface, size cannot be inferred")
	}

	size := contentSizeAware.Size()
	if size == blob.SizeUnknown {
		return nil, fmt.Errorf("content size is unknown")
	}

	layer, err := createLayer(access, size, resource)
	if err != nil {
		return nil, fmt.Errorf("error creating layer descriptor: %v", err)
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
		return nil, fmt.Errorf("component and version must not be empty")
	}
	if len(identity) == 0 {
		return nil, fmt.Errorf("identity must not be empty")
	}

	reference := repo.resolver.ComponentVersionReference(component, version)
	store, err := repo.resolver.StoreForReference(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("failed to get store for reference: %w", err)
	}

	manifest, err := getManifest(ctx, store, reference)
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
	descriptorEncoding, descriptorBuffer, err := singleFileTAREncodeDescriptor(descriptor)
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
	if err := store.Push(ctx, descriptorOCIDescriptor, content.NewVerifyReader(
		bytes.NewReader(descriptorBytes),
		descriptorOCIDescriptor,
	)); err != nil {
		return fmt.Errorf("unable to push component descriptor: %w", err)
	}

	// Create and upload the component configuration
	componentConfigRaw, componentConfigDescriptor, err := createComponentConfig(descriptorOCIDescriptor)
	if err != nil {
		return fmt.Errorf("failed to marshal component config: %w", err)
	}
	logger.Log(ctx, slog.LevelDebug, "pushing descriptor", descriptorLogAttr(componentConfigDescriptor))
	if err := store.Push(ctx, componentConfigDescriptor, content.NewVerifyReader(
		bytes.NewReader(componentConfigRaw),
		componentConfigDescriptor,
	)); err != nil {
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
			"software.ocm.componentversion": fmt.Sprintf("component-descriptors/%s:%s", component, version),
			"software.ocm.creator":          "OCM OCI Repository Plugin (POCM)",
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
	if err := store.Push(ctx, manifestDescriptor, content.NewVerifyReader(
		bytes.NewReader(manifestRaw),
		manifestDescriptor,
	)); err != nil {
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

	manifest, err := getManifest(ctx, store, reference)
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
	descriptorRaw, err := store.Fetch(ctx, componentConfig.ComponentDescriptorLayer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch descriptor layer: %w", err)
	}
	defer func() {
		_ = descriptorRaw.Close()
	}()

	return singleFileTARDecodeDescriptor(descriptorRaw)
}

// UploadResource uploads a resource to the repository.
func (repo *Repository) UploadResource(ctx context.Context, res *descriptor.Resource, b blob.ReadOnlyBlob) (newRes *descriptor.Resource, err error) {
	done := logOperation(ctx, "upload resource", slog.String("resource", res.Name))
	defer done(err)

	var access v1.OCIImage
	if err := repo.scheme.Convert(res.Access, &access); err != nil {
		return nil, fmt.Errorf("error converting resource access to OCI image: %w", err)
	}

	targetRef, err := repo.resolver.TargetResourceReference(access.ImageReference)
	if err != nil {
		return nil, err
	}
	store, err := repo.resolver.StoreForReference(ctx, targetRef)
	if err != nil {
		return nil, err
	}

	fileBufferPath, err := prepareResourceFile(res, b)
	if err != nil {
		return nil, err
	}

	desc, err := copyResource(ctx, fileBufferPath, access.ImageReference, targetRef, store)
	if err != nil {
		return nil, errors.Join(os.Remove(fileBufferPath), err)
	}
	defer os.Remove(fileBufferPath)

	res.Size = desc.Size
	res.Digest = &descriptor.Digest{
		HashAlgorithm: digest.SHA256.String(),
		Value:         desc.Digest.Encoded(),
	}
	access.ImageReference = targetRef

	return res, nil
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

	var buf bytes.Buffer
	zippedBuf := gzip.NewWriter(&buf)
	defer func() {
		if err != nil {
			// Clean up resources if there was an error
			zippedBuf.Close()
			buf.Reset()
		}
	}()

	target := NewOCILayoutTarWriter(zippedBuf)
	defer func() {
		if err := target.Close(); err != nil {
			err = errors.Join(err, fmt.Errorf("failed to close tar writer: %w", err))
			return
		}
		if err := zippedBuf.Close(); err != nil {
			err = errors.Join(err, fmt.Errorf("failed to close gzip writer: %w", err))
			return
		}
	}()

	desc, err := copyResourceToTarget(ctx, src, access.ImageReference, target)
	if err != nil {
		return nil, fmt.Errorf("failed to copy resource: %w", err)
	}

	describedBlob := NewDescriptorBlob(&buf, desc)
	mediaType, ok := describedBlob.MediaType()
	if !ok {
		return nil, fmt.Errorf("failed to get media type")
	}
	return NewResourceBlob(res, describedBlob, mediaType), nil
}

// copyResourceToTarget copies a resource from the store to a buffer.
func copyResourceToTarget(ctx context.Context, store Store, srcRef string, storage CloseableTarget) (ociImageSpecV1.Descriptor, error) {
	return oras.Copy(ctx, store, srcRef, storage, srcRef, oras.CopyOptions{
		CopyGraphOptions: oras.CopyGraphOptions{
			Concurrency: 8,
			PreCopy: func(ctx context.Context, desc ociImageSpecV1.Descriptor) error {
				slog.DebugContext(ctx, "downloading", slog.String("descriptor", desc.Digest.String()), slog.String("mediaType", desc.MediaType))
				return nil
			},
			PostCopy: func(ctx context.Context, desc ociImageSpecV1.Descriptor) error {
				slog.InfoContext(ctx, "downloaded", slog.String("descriptor", desc.Digest.String()), slog.String("mediaType", desc.MediaType))
				return nil
			},
			OnCopySkipped: func(ctx context.Context, desc ociImageSpecV1.Descriptor) error {
				slog.DebugContext(ctx, "skipped", slog.String("descriptor", desc.Digest.String()), slog.String("mediaType", desc.MediaType))
				return nil
			},
		},
	})
}

// getManifest retrieves the manifest for a given reference from the store.
func getManifest(ctx context.Context, store Store, reference string) (manifest ociImageSpecV1.Manifest, err error) {
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

// singleFileTARDecodeDescriptor decodes a component descriptor from a TAR archive.
func singleFileTARDecodeDescriptor(raw io.Reader) (*descriptor.Descriptor, error) {
	const descriptorFileHeader = "component-descriptor.yaml"

	tarReader := tar.NewReader(raw)
	var buf bytes.Buffer
	found := false

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar header: %w", err)
		}

		switch header.Name {
		case descriptorFileHeader:
			if found {
				return nil, fmt.Errorf("multiple component-descriptor.yaml files found")
			}
			found = true
			if _, err := io.Copy(&buf, tarReader); err != nil {
				return nil, fmt.Errorf("reading component descriptor: %w", err)
			}
		default:
			if _, err := io.Copy(io.Discard, tarReader); err != nil {
				return nil, fmt.Errorf("skipping file %s: %w", header.Name, err)
			}
		}
	}

	if !found {
		return nil, fmt.Errorf("component-descriptor.yaml not found in archive")
	}

	var desc descriptor.Descriptor
	if err := yaml.Unmarshal(buf.Bytes(), &desc); err != nil {
		return nil, fmt.Errorf("unmarshaling component descriptor: %w", err)
	}

	return &desc, nil
}

// singleFileTAREncodeDescriptor encodes a component descriptor into a TAR archive.
func singleFileTAREncodeDescriptor(desc *descriptor.Descriptor) (encoding string, _ *bytes.Buffer, err error) {
	descriptorEncoding := "+yaml"
	descriptorYAML, err := yaml.Marshal(desc)
	if err != nil {
		return "", nil, fmt.Errorf("unable to encode component descriptor: %w", err)
	}
	// prepare the descriptor
	descriptorEncoding += "+tar"
	var descriptorBuffer bytes.Buffer
	tarWriter := tar.NewWriter(&descriptorBuffer)
	defer func() {
		err = errors.Join(err, tarWriter.Close())
	}()

	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "component-descriptor.yaml",
		Mode: 0644,
		Size: int64(len(descriptorYAML)),
	}); err != nil {
		return "", nil, fmt.Errorf("unable to write component descriptor header: %w", err)
	}
	if _, err := io.Copy(tarWriter, bytes.NewReader(descriptorYAML)); err != nil {
		return "", nil, err
	}
	return descriptorEncoding, &descriptorBuffer, nil
}

// logOperation is a helper function to log operations with timing and error handling.
func logOperation(ctx context.Context, operation string, fields ...slog.Attr) func(error) {
	start := time.Now()
	attrs := make([]any, 0, len(fields)+1)
	attrs = append(attrs, slog.String("operation", operation))
	for _, field := range fields {
		attrs = append(attrs, field)
	}
	logger := logger.With(attrs...)
	logger.Log(ctx, slog.LevelInfo, "starting operation")
	return func(err error) {
		if err != nil {
			logger.Log(ctx, slog.LevelError, "operation failed", slog.Duration("duration", time.Since(start)), slog.String("error", err.Error()))
		} else {
			logger.Log(ctx, slog.LevelInfo, "operation completed", slog.Duration("duration", time.Since(start)))
		}
	}
}

// descriptorLogAttr creates a log attribute for an OCI descriptor.
func descriptorLogAttr(descriptor ociImageSpecV1.Descriptor) slog.Attr {
	return slog.Group("descriptor",
		slog.String("mediaType", descriptor.MediaType),
		slog.String("digest", descriptor.Digest.String()),
		slog.Int64("size", descriptor.Size),
	)
}

// createLayer creates an OCI layer descriptor for a resource.
func createLayer(access *v2.LocalBlob, size int64, resource *descriptor.Resource) (ociImageSpecV1.Descriptor, error) {
	layer := ociImageSpecV1.Descriptor{
		MediaType: access.MediaType,
		Digest:    digest.Digest(access.LocalReference),
		Size:      size,
	}

	identity := resource.ToIdentity()
	special := map[string]func(platform *ociImageSpecV1.Platform, value string){
		"architecture": func(platform *ociImageSpecV1.Platform, value string) {
			platform.Architecture = value
			return
		},
		"os": func(platform *ociImageSpecV1.Platform, value string) {
			platform.OS = value
			return
		},
		"variant": func(platform *ociImageSpecV1.Platform, value string) {
			platform.Variant = value
			return
		},
		"os.features": func(platform *ociImageSpecV1.Platform, value string) {
			platform.OSFeatures = strings.Split(value, ",")
			return
		},
		"os.version": func(platform *ociImageSpecV1.Platform, value string) {
			platform.OSVersion = value
			return
		},
	}
	for key, value := range resource.ExtraIdentity {
		if set, ok := special[key]; ok {
			if layer.Platform == nil {
				layer.Platform = &ociImageSpecV1.Platform{}
			}
			set(layer.Platform, value)
		}
	}

	if err := (&ArtifactOCILayerAnnotation{
		Identity: identity,
		Kind:     ArtifactKindResource,
	}).AddToDescriptor(&layer); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to add resource artifact annotation to descriptor: %w", err)
	}

	return layer, nil
}

// updateResourceAccess updates the resource access with the new layer information.
func updateResourceAccess(resource *descriptor.Resource, layer ociImageSpecV1.Descriptor) error {
	if resource == nil {
		return fmt.Errorf("resource must not be nil")
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

// prepareResourceFile creates a temporary file with the resource content.
func prepareResourceFile(resource *descriptor.Resource, content blob.ReadOnlyBlob) (path string, err error) {
	filePath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d", resource.Name, time.Now().UnixNano()))
	tmpFile, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
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
	if err != nil && err != io.EOF && !errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("failed to read data for gzip detection: %w", err)
	}

	// Reconstruct reader with the first two bytes prepended
	var reader = io.MultiReader(bytes.NewReader(header[:n]), src)

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

// copyResource copies a resource from the source to the target store.
func copyResource(ctx context.Context, srcPath, srcRef, targetRef string, store Store) (ociImageSpecV1.Descriptor, error) {
	src, err := oci.NewFromTar(ctx, srcPath)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}

	return oras.Copy(ctx, src, srcRef, store, targetRef, oras.CopyOptions{
		CopyGraphOptions: oras.CopyGraphOptions{
			PreCopy: func(ctx context.Context, desc ociImageSpecV1.Descriptor) error {
				slog.DebugContext(ctx, "uploading", slog.String("descriptor", desc.Digest.String()), slog.String("mediaType", desc.MediaType))
				return nil
			},
			PostCopy: func(ctx context.Context, desc ociImageSpecV1.Descriptor) error {
				slog.DebugContext(ctx, "uploaded", slog.String("descriptor", desc.Digest.String()), slog.String("mediaType", desc.MediaType))
				return nil
			},
			OnCopySkipped: func(ctx context.Context, desc ociImageSpecV1.Descriptor) error {
				slog.DebugContext(ctx, "skipped", slog.String("descriptor", desc.Digest.String()), slog.String("mediaType", desc.MediaType))
				return nil
			},
		},
	})
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
