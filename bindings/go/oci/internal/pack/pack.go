// Package pack provides functionality for creating and managing OCI artifacts based on resources and blobs.
// It supports packing resources into OCI-compliant artifacts and pushing them to OCI registries.
package pack

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	resourceblob "ocm.software/open-component-model/bindings/go/oci/blob"
	"ocm.software/open-component-model/bindings/go/oci/internal/identity"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	digestv1 "ocm.software/open-component-model/bindings/go/oci/spec/digest/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// AnnotationSingleLayerArtifact is the annotation key used to identify single-layer artifacts.
// If set, it contains the digest of the single layer packaged within the manifest.
// It is set on the manifest and not on the layer itself.
const AnnotationSingleLayerArtifact = "software.ocm.artifact.singlelayer"

// LocalResourceAdoptionMode defines how local resources should be accessed in the repository.
type LocalResourceAdoptionMode int

func (l LocalResourceAdoptionMode) String() string {
	switch l {
	case LocalResourceAdoptionModeLocalBlobWithNestedGlobalAccess:
		return "localBlobWithNestedGlobalAccess"
	case LocalResourceAdoptionModeOCIImage:
		return "ociImage"
	default:
		return fmt.Sprintf("unknown (%d)", l)
	}
}

const (
	// LocalResourceAdoptionModeLocalBlobWithNestedGlobalAccess creates a local blob access for resources.
	// It also embeds the global access information in the local blob.
	LocalResourceAdoptionModeLocalBlobWithNestedGlobalAccess LocalResourceAdoptionMode = iota
	// LocalResourceAdoptionModeOCIImage creates an OCI image layer access for resources.
	// This mode is used when the resource is embedded without a local blob (only global access)
	LocalResourceAdoptionModeOCIImage LocalResourceAdoptionMode = iota
)

// Options defines the configuration options for packing a single-layer OCI artifact.
type Options struct {
	// AccessScheme is the scheme used for converting resource access types.
	AccessScheme *runtime.Scheme

	// CopyGraphOptions are the options for copying resource graphs when dealing with OCI layouts.
	CopyGraphOptions oras.CopyGraphOptions

	// BaseReference is the base reference for the resource access that is used to update the resource.
	BaseReference string

	// LocalResourceAdoptionMode defines how local resources should be modified when packed.
	LocalResourceAdoptionMode LocalResourceAdoptionMode

	// ManifestAnnotations are annotations that will be added to single layer Artifacts
	// They are not used for OCI Layouts.
	ManifestAnnotations map[string]string
}

// ResourceBlob packs a resourceblob.ResourceBlob into an OCI Storage
func ResourceBlob(ctx context.Context, storage content.Storage, b *resourceblob.ResourceBlob, opts Options) (desc ociImageSpecV1.Descriptor, err error) {
	access := b.Resource.Access
	if access == nil || access.GetType().IsEmpty() {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("resource access or access type is empty")
	}
	typed, err := opts.AccessScheme.NewObject(access.GetType())
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("error creating resource access: %w", err)
	}
	if err := opts.AccessScheme.Convert(access, typed); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("error converting resource access: %w", err)
	}

	switch access := typed.(type) {
	case *v2.LocalBlob:
		internal, err := descriptor.ConvertFromV2LocalBlob(opts.AccessScheme, access)
		if err != nil {
			return ociImageSpecV1.Descriptor{}, fmt.Errorf("error converting resource local blob access in version 2 to internal representation: %w", err)
		}
		return ResourceLocalBlob(ctx, storage, b, internal, opts)
	default:
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("unsupported access type: %T", access)
	}
}

func ResourceLocalBlob(ctx context.Context, storage content.Storage, b *resourceblob.ResourceBlob, access *descriptor.LocalBlob, opts Options) (desc ociImageSpecV1.Descriptor, err error) {
	switch mediaType := access.MediaType; mediaType {
	case layout.MediaTypeOCIImageLayoutTarV1, layout.MediaTypeOCIImageLayoutTarGzipV1:
		return ResourceLocalBlobOCILayout(ctx, storage, b, opts)
	default:
		return ResourceLocalBlobOCISingleLayerArtifact(ctx, storage, b, access, opts)
	}
}

func ResourceLocalBlobOCISingleLayerArtifact(ctx context.Context, storage content.Storage, b *resourceblob.ResourceBlob, access *descriptor.LocalBlob, opts Options) (ociImageSpecV1.Descriptor, error) {
	layer, err := NewResourceBlobOCILayer(b, ResourceBlobOCILayerOptions{
		BlobMediaType: access.MediaType,
		BlobDigest:    digest.Digest(access.LocalReference),
	})
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to create resource layer based on blob: %w", err)
	}

	if err := Blob(ctx, storage, b, layer); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to push blob: %w", err)
	}

	annotations := maps.Clone(layer.Annotations)
	maps.Copy(annotations, opts.ManifestAnnotations)
	annotations[AnnotationSingleLayerArtifact] = layer.Digest.String()

	desc, err := oras.PackManifest(ctx, storage, oras.PackManifestVersion1_1, access.MediaType, oras.PackManifestOptions{
		Layers:              []ociImageSpecV1.Descriptor{layer},
		ManifestAnnotations: annotations,
	})
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to pack manifest: %w", err)
	}

	if err := updateResourceAccess(b.Resource, desc, opts); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to update resource access: %w", err)
	}

	return desc, nil
}

func ResourceLocalBlobOCILayout(ctx context.Context, storage content.Storage, b *resourceblob.ResourceBlob, opts Options) (ociImageSpecV1.Descriptor, error) {
	index, err := tar.CopyOCILayoutWithIndex(ctx, storage, b, tar.CopyOCILayoutWithIndexOptions{
		CopyGraphOptions: opts.CopyGraphOptions,
		MutateParentFunc: func(idx *ociImageSpecV1.Descriptor) error {
			return identity.AdoptAsResource(idx, b.Resource)
		},
	})
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to copy OCI layout: %w", err)
	}
	if err := updateResourceAccess(b.Resource, index, opts); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to update resource access: %w", err)
	}
	return index, nil
}

// ResourceBlobOCILayerOptions defines the configuration options for pushing a blob as a resource.
type ResourceBlobOCILayerOptions struct {
	// BlobMediaType specifies the media type of the blob, if not specified blob.MediaTypeAware interface will be used
	BlobMediaType string
	// BlobDigest is the digest of the blob, if not specified blob.DigestAware interface will be used
	BlobDigest digest.Digest
	// BlobLayerAnnotations contains additional annotations for the layer
	BlobLayerAnnotations map[string]string
}

// NewResourceBlobOCILayer creates a new OCI layer descriptor for a resource blob.
func NewResourceBlobOCILayer(b *resourceblob.ResourceBlob, opts ResourceBlobOCILayerOptions) (ociImageSpecV1.Descriptor, error) {
	size := b.Size()
	if size == blob.SizeUnknown {
		return ociImageSpecV1.Descriptor{}, errors.New("blob size is unknown and cannot be packed into a single layer artifact")
	}

	var mediaType string
	if mediaTypeFromBlob, ok := b.MediaType(); ok {
		mediaType = mediaTypeFromBlob
	}
	if mediaType == "" {
		mediaType = opts.BlobMediaType
	}
	if mediaType == "" {
		return ociImageSpecV1.Descriptor{}, errors.New("blob media type is unknown and cannot be packed into an oci blob")
	}

	var dig digest.Digest
	if blobDigest, ok := b.Digest(); ok {
		dig = digest.Digest(blobDigest)
		if err := dig.Validate(); err != nil {
			return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to validate blob digest: %w", err)
		}
	}
	if len(dig) == 0 {
		dig = opts.BlobDigest
		if err := dig.Validate(); err != nil {
			return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to validate blob digest: %w", err)
		}
	}

	layer := ociImageSpecV1.Descriptor{
		MediaType:   mediaType,
		Digest:      dig,
		Annotations: opts.BlobLayerAnnotations,
		Size:        size,
	}

	if err := identity.AdoptAsResource(&layer, b.Resource); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to adopt descriptor based on resource: %w", err)
	}

	return layer, nil
}

// Blob handles the actual transfer of blob data to the OCI storage.
// It reads the blob content and pushes it to the storage using the provided descriptor.
// The function ensures proper cleanup of resources by closing the blob reader after the transfer.
func Blob(ctx context.Context, storage content.Pusher, b blob.ReadOnlyBlob, desc ociImageSpecV1.Descriptor) error {
	layerData, err := b.ReadCloser()
	if err != nil {
		return fmt.Errorf("failed to get blob reader: %w", err)
	}
	defer func() {
		err = errors.Join(err, layerData.Close())
	}()

	if err := storage.Push(ctx, desc, layerData); err != nil {
		return fmt.Errorf("failed to push layer: %w", err)
	}

	return nil
}

// updateResourceAccess updates the resource access with the new layer information.
// for setting a global access it uses the base reference given which must not already contain a digest.
func updateResourceAccess(resource *descriptor.Resource, desc ociImageSpecV1.Descriptor, opts Options) error {
	if resource == nil {
		return errors.New("resource must not be nil")
	}

	access := &accessv1.OCIImage{
		// This is the target image reference under which the resource will be accessible once
		// added to the OCM Component Version Repository. Note that this reference will not work
		// unless the component version is actually updated.
		ImageReference: fmt.Sprintf("%s@%s", opts.BaseReference, desc.Digest.String()),
	}

	// Create access based on configured mode
	switch opts.LocalResourceAdoptionMode {
	case LocalResourceAdoptionModeOCIImage:
		resource.Access = access
	case LocalResourceAdoptionModeLocalBlobWithNestedGlobalAccess:
		// Create local blob access
		access, err := descriptor.ConvertToV2LocalBlob(opts.AccessScheme, &descriptor.LocalBlob{
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
		return fmt.Errorf("unsupported access mode: %s", opts.LocalResourceAdoptionMode)
	}

	if err := digestv1.ApplyToResource(resource, desc.Digest, digestv1.OCIArtifactDigestAlgorithm); err != nil {
		return fmt.Errorf("failed to apply digest to resource: %w", err)
	}

	return nil
}
