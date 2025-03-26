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

const (
	MediaTypeComponentDescriptor   = "application/vnd.ocm.software/ocm.component-descriptor"
	MediaTypeComponentDescriptorV2 = MediaTypeComponentDescriptor + ".v2"
)

var logger = slog.With(slog.String("realm", "oci"))

type LocalBlob interface {
	blob.ReadOnlyBlob
	blob.SizeAware
	blob.DigestAware
	blob.MediaTypeAware
}

// OCMComponentVersionRepository is a repository that can store and retrieve Component Descriptors based on a
// component version, as well as store correlated data (local resources) that are stored next to the component version.
type OCMComponentVersionRepository interface {
	// AddComponentVersion adds a new component version to the repository.
	// If the component under that version exists, it is expected that once this call returns successfully,
	// the component version is available for retrieval with the new descriptor.
	AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error
	// GetComponentVersion retrieves a component version from the repository. It will contain the descriptor
	// from the last AddComponentVersion call made to that component and version.
	GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error)
	// AddLocalResource adds a local resource to the repository. compared to AddComponentVersion,
	// the resource is not an identifier on its own, so storing a resource for a component version that does not
	// yet exist can be done, but may not be persisted beyond a garbage collection that removes unreferenced resources.
	// note that the identity needs to match an identity in the component descriptor for local resources.
	AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (newRes *descriptor.Resource, err error)
	// GetLocalResource retrieves a local resource from the repository. The identity is used to determine which resource
	// to retrieve. If the identity does not match any resource in the descriptor, there is no guarantee that the resource
	// can be returned.
	GetLocalResource(ctx context.Context, component, version string, identity map[string]string) (LocalBlob, error)
}

// OCMResourceRepository is a repository that can store and retrieve resources independently of component versions.
// It can be used to store resources that are not directly associated with a component version, but also to transfer
// resources between repositories that may not be stored alongside the component version itself
type OCMResourceRepository interface {
	UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob) (newRes *descriptor.Resource, err error)
	DownloadResource(ctx context.Context, res *descriptor.Resource) (content blob.ReadOnlyBlob, err error)
}

// Resolver resolves references and stores.
type Resolver interface {
	// StoreForReference resolves a reference to a Store.
	// For each ComponentVersion, a repository is able to resolve a different store.
	StoreForReference(ctx context.Context, reference string) (Store, error)
	// ComponentVersionReference returns a reference for a component version.
	// This is a unique reference that can be used within the repository to refer back to the version.
	ComponentVersionReference(component, version string) string
	// TargetResourceReference returns a reference for a resource that can be used to upload the resource to the repository.
	TargetResourceReference(srcReference string) (targetReference string, err error)
}

type Store interface {
	content.ReadOnlyStorage
	content.Pusher
	content.TagResolver
	content.Tagger
}

// Repository is an OCMComponentVersionRepository backed by a set of OCI Repositories that are accessible through
// the provided Resolver.
// Each component version is resolved to a repository Store that is used to store the component version.
type Repository struct {
	scheme *runtime.Scheme

	// localBlobMemory is a map that stores local blobs in memory until they are added to a component version.
	localBlobMemory LocalBlobMemory

	// resolver is used to resolve component version references to stores so that they can be fetched and uploaded.
	resolver Resolver
}

func RepositoryFromResolverAndMemory(resolver Resolver, blobMemory LocalBlobMemory) *Repository {
	scheme := runtime.NewScheme()
	ocmoci.MustAddToScheme(scheme)
	v2.MustAddToScheme(scheme)
	return &Repository{
		resolver:        resolver,
		localBlobMemory: blobMemory,
		scheme:          scheme,
	}
}

// LocalBlobMemory is a map that stores local blobs in memory until they are added to a component version.
// TODO: make this a file cacher similar to the OCI Layout "ingest" directory of ORAS.
type LocalBlobMemory map[string][]ociImageSpecV1.Descriptor

func NewLocalBlobMemory() LocalBlobMemory {
	return make(map[string][]ociImageSpecV1.Descriptor)
}

var _ OCMComponentVersionRepository = (*Repository)(nil)

func (repo *Repository) AddLocalResource(
	ctx context.Context,
	component, version string,
	resource *descriptor.Resource,
	content blob.ReadOnlyBlob,
) (newRes *descriptor.Resource, err error) {
	reference := repo.resolver.ComponentVersionReference(component, version)
	store, err := repo.resolver.StoreForReference(ctx, reference)
	if err != nil {
		return nil, err
	}

	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required for uploading to an OCI repository")
	}

	var access v2.LocalBlob
	if err := repo.scheme.Convert(resource.Access, &access); err != nil {
		return nil, fmt.Errorf("error converting resource access to OCI image: %w", err)
	}

	if access.MediaType == "" {
		return nil, fmt.Errorf("resource access media type is required for uploading to an OCI repository")
	}
	layerDigest := digest.Digest(access.LocalReference)
	if err := layerDigest.Validate(); err != nil {
		return nil, fmt.Errorf("invalid layer digest in local reference: %w", err)
	}

	size := content.(blob.SizeAware).Size()

	layer := ociImageSpecV1.Descriptor{
		MediaType: access.MediaType,
		Digest:    layerDigest,
		Size:      size,
	}

	identity := resource.ToIdentity()
	applyLayerToIdentity(resource, layer)

	if err := (&ArtifactOCILayerAnnotation{
		Identity: identity,
		Kind:     ArtifactKindResource,
	}).AddToDescriptor(&layer); err != nil {
		return nil, err
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
	repo.localBlobMemory[reference] = append(repo.localBlobMemory[reference], layer)

	resource.Access = &descriptor.LocalBlob{
		LocalReference: layer.Digest.String(),
		MediaType:      layer.MediaType,
		GlobalAccess: &v1.OCIImageLayer{
			Digest:    layer.Digest,
			MediaType: layer.MediaType,
			Reference: fmt.Sprintf("%s@%s", reference, layer.Digest.String()),
			Size:      layer.Size,
		},
	}
	if err := ociDigestV1.ApplyToResource(resource, layer.Digest); err != nil {
		return nil, fmt.Errorf("error applying digest to resource: %w", err)
	}

	return resource, nil
}

func (repo *Repository) GetLocalResource(ctx context.Context, component, version string, identity map[string]string) (LocalBlob, error) {
	reference := repo.resolver.ComponentVersionReference(component, version)
	store, err := repo.resolver.StoreForReference(ctx, reference)
	if err != nil {
		slog.Info("failed to resolve store for reference", "reference", reference, "error", err)
		return nil, err
	}

	manifest, err := getManifest(ctx, store, reference)
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest: %w", err)
	}

	var matchingLayers []ociImageSpecV1.Descriptor
	var notMatched []ociImageSpecV1.Descriptor
	for _, layer := range manifest.Layers {
		artifactAnnotations, err := GetArtifactOCILayerAnnotations(&layer)
		if errors.Is(err, ErrArtifactOCILayerAnnotationDoesNotExist) || len(artifactAnnotations) == 0 {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("error getting artifact annotation: %w", err)
		}
		required := identity
		matched := 0

		for _, artifactAnnotation := range artifactAnnotations {
			if artifactAnnotation.Kind != ArtifactKindResource {
				continue
			}
			if runtime.Identity(required).Match(artifactAnnotation.Identity) {
				matchingLayers = append(matchingLayers, layer)
				matched++
			} else {
				notMatched = append(notMatched, layer)
			}
		}

		if matched > 0 {
			break
		}
	}

	if len(matchingLayers) == 0 {
		return nil, fmt.Errorf("no matching layers for identity %v (not matched other layers %v): %w", identity, notMatched, errdef.ErrNotFound)
	} else if len(matchingLayers) > 1 {
		return nil, fmt.Errorf("found multiple matching layers for identity %v", identity)
	}

	data, err := store.Fetch(ctx, matchingLayers[0])
	if err != nil {
		return nil, err
	}

	return NewDescriptorBlob(data, matchingLayers[0]), nil
}

func (repo *Repository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) (err error) {
	component, version := descriptor.Component.Name, descriptor.Component.Version

	logger := logger.With(slog.String("component", component), slog.String("version", version))
	logger.Log(ctx, slog.LevelInfo, "adding component version")
	start := time.Now()
	defer func() {
		if err != nil {
			logger.Log(ctx, slog.LevelError, "failed to add component version", slog.Duration("duration", time.Since(start)), slog.String("error", err.Error()))
		} else {
			logger.Log(ctx, slog.LevelInfo, "added component version", slog.Duration("duration", time.Since(start)))
		}
	}()

	// ResolveRepositoryCredentials the reference and obtain the appropriate store.
	reference := repo.resolver.ComponentVersionReference(component, version)
	store, err := repo.resolver.StoreForReference(ctx, reference)
	if err != nil {
		return fmt.Errorf("failed to resolve store for reference: %w", err)
	}
	logger = logger.With("reference", reference)

	// Encode and upload the descriptor.
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

	// Create and upload the component configuration.
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

	// Create and upload the manifest.
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
			repo.localBlobMemory[reference]...,
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

	// Tag the manifest with the reference.
	if err := store.Tag(ctx, manifestDescriptor, reference); err != nil {
		return fmt.Errorf("failed to tag manifest: %w", err)
	}

	// Cleanup local blob memory because now all local layers have been pushed
	delete(repo.localBlobMemory, reference)

	return nil
}

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

	// 5) read component descriptor
	descriptorRaw, err := store.Fetch(ctx, componentConfig.ComponentDescriptorLayer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch descriptor layer: %w", err)
	}
	defer func() {
		_ = descriptorRaw.Close()
	}()

	return singleFileTARDecodeDescriptor(descriptorRaw)
}

func (repo *Repository) UploadResource(ctx context.Context, res *descriptor.Resource, b blob.ReadOnlyBlob) (newRes *descriptor.Resource, err error) {
	start := time.Now()
	logger := logger.With(slog.String("resource", res.Name))
	defer func() {
		if err != nil {
			logger.Log(ctx, slog.LevelError, "failed to upload resource", slog.Duration("duration", time.Since(start)))
		} else {
			logger.Log(ctx, slog.LevelInfo, "uploaded resource", slog.Duration("duration", time.Since(start)))
		}
	}()
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

	fileBufferPath := filepath.Join(os.TempDir(), res.Name)
	tmp, err := os.OpenFile(fileBufferPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, tmp.Close(), os.Remove(fileBufferPath))
	}()

	data, err := b.ReadCloser()
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, data.Close())
	}()
	unzippedData, err := gzip.NewReader(data)
	if err != nil {
		return nil, err
	}

	if _, err := io.Copy(tmp, unzippedData); err != nil {
		return nil, err
	}

	// TODO determine a small enough size for upload at which we can keep the whole resource in memory
	//   Then implement a direct reader from an io.Reader instead of a fileBuffer (not part of ORAS core lib)
	src, err := oci.NewFromTar(ctx, fileBufferPath)
	if err != nil {
		return nil, err
	}

	desc, err := oras.Copy(ctx, src, access.ImageReference, store, targetRef, oras.CopyOptions{
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
	if err != nil {
		return nil, err
	}

	res.Size = desc.Size
	res.Digest = &descriptor.Digest{
		HashAlgorithm: digest.SHA256.String(),
		Value:         desc.Digest.Encoded(),
	}
	access.ImageReference = targetRef

	return res, nil
}

func (repo *Repository) DownloadResource(ctx context.Context, res *descriptor.Resource) (data blob.ReadOnlyBlob, err error) {
	start := time.Now()
	logger := logger.With(slog.String("resource", res.Name))
	defer func() {
		if err != nil {
			logger.Log(ctx, slog.LevelError, "failed to download resource", slog.Duration("duration", time.Since(start)))
		} else {
			logger.Log(ctx, slog.LevelInfo, "downloaded resource", slog.Duration("duration", time.Since(start)))
		}
	}()
	var access v1.OCIImage
	if err := repo.scheme.Convert(res.Access, &access); err != nil {
		return nil, fmt.Errorf("error converting resource access to OCI image: %w", err)
	}
	store, err := repo.resolver.StoreForReference(ctx, access.ImageReference)
	if err != nil {
		return nil, err
	}

	// TODO determine a big enough size for download at which we cannot keep the whole resource in memory
	//   Then offload to a file instead of a buffer.
	var buf bytes.Buffer
	zippedBuf := gzip.NewWriter(&buf)
	defer func() {
		err = errors.Join(err, zippedBuf.Close())
	}()
	storage := NewOCILayoutTarWriter(zippedBuf)
	defer func() {
		err = errors.Join(err, storage.Close())
	}()

	desc, err := oras.Copy(ctx, store, access.ImageReference, storage, access.ImageReference, oras.CopyOptions{
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
	if err != nil {
		return nil, fmt.Errorf("failed to copy resource: %w", err)
	}
	describedBlob := NewDescriptorBlob(&buf, desc)
	mediaType, _ := describedBlob.MediaType()
	return NewResourceBlob(res, describedBlob, mediaType), nil
}

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

func singleFileTARDecodeDescriptor(raw io.Reader) (desc *descriptor.Descriptor, err error) {
	tarReader := tar.NewReader(raw)
	header, err := tarReader.Next()
	if err != nil {
		return nil, err
	}
	if header.Name != "component-descriptor.yaml" {
		return nil, fmt.Errorf("unexpected tar entry name: %s", header.Name)
	}
	descriptorBuffer := bytes.Buffer{}
	if _, err := io.Copy(&descriptorBuffer, tarReader); err != nil {
		return nil, err
	}
	var decoded descriptor.Descriptor
	if err := yaml.Unmarshal(descriptorBuffer.Bytes(), &decoded); err != nil {
		return nil, err
	}

	return &decoded, nil
}

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

func descriptorLogAttr(descriptor ociImageSpecV1.Descriptor) slog.Attr {
	return slog.Group("descriptor",
		slog.String("mediaType", descriptor.MediaType),
		slog.String("digest", descriptor.Digest.String()),
		slog.Int64("size", descriptor.Size),
	)
}

func applyLayerToIdentity(resource *descriptor.Resource, layer ociImageSpecV1.Descriptor) {
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
}
