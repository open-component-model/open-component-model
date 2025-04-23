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
	"oras.land/oras-go/v2/registry/remote"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	resourceblob "ocm.software/open-component-model/bindings/go/oci/blob"
	internaldigest "ocm.software/open-component-model/bindings/go/oci/internal/digest"
	"ocm.software/open-component-model/bindings/go/oci/internal/identity"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Options defines the configuration options for packing a single-layer OCI artifact.
type Options struct {
	// AccessScheme is the scheme used for converting resource access types.
	AccessScheme *runtime.Scheme

	// CopyGraphOptions are the options for copying resource graphs when dealing with OCI layouts.
	CopyGraphOptions oras.CopyGraphOptions

	// BaseReference is the base reference for the resource access that is used to update the resource.
	BaseReference string

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
		return ResourceLocalBlobOCILayer(ctx, storage, b, access, opts)
	}
}

func ResourceLocalBlobOCILayer(ctx context.Context, storage content.Storage, b *resourceblob.ResourceBlob, access *descriptor.LocalBlob, opts Options) (ociImageSpecV1.Descriptor, error) {
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

	global := backedByGlobalStore(storage)

	if err := updateResourceAccess(b.Resource, layer, updateResourceAccessOptions{opts, global}); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to update resource access: %w", err)
	}

	return layer, nil
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
	global := backedByGlobalStore(storage)
	if err := updateResourceAccess(b.Resource, index, updateResourceAccessOptions{opts, global}); err != nil {
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

type updateResourceAccessOptions struct {
	Options
	// BackedByGlobalStore indicates if the resource is backed by a global store.
	// This is used to determine if the resource access should be updated with a global reference.
	BackedByGlobalStore bool
}

// updateResourceAccess updates the resource access with the new layer information.
// for setting a global access it uses the base reference given which must not already contain a digest.
func updateResourceAccess(resource *descriptor.Resource, desc ociImageSpecV1.Descriptor, opts updateResourceAccessOptions) error {
	if resource == nil {
		return errors.New("resource must not be nil")
	}

	localBlob := &descriptor.LocalBlob{
		Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
		LocalReference: desc.Digest.String(),
		MediaType:      desc.MediaType,
	}

	if opts.BackedByGlobalStore {
		localBlob.GlobalAccess = &accessv1.OCIImage{
			// This is an absolute reference to the blob in the global store.
			// It contains the base reference and the digest of the blob to form an absolute, pinned (by digest)
			// OCI reference.
			ImageReference: fmt.Sprintf("%s@%s", opts.BaseReference, desc.Digest.String()),
		}
	}

	// convert to apply the access
	access, err := descriptor.ConvertToV2LocalBlob(opts.AccessScheme, localBlob)
	if err != nil {
		return fmt.Errorf("failed to convert access to local blob: %w", err)
	}
	resource.Access = access

	if err := internaldigest.ApplyToResource(resource, desc.Digest); err != nil {
		return fmt.Errorf("failed to apply digest to resource: %w", err)
	}

	return nil
}

// backedByGlobalStore checks if the given storage is backed by a globally reachable store
//
// TODO(jakobmoellerdev): Eventually we should find a smarter solution to determine if a store is global.
func backedByGlobalStore(storage content.Storage) bool {
	switch storage.(type) {
	// for ORAS repositories, we know they are global if they are remote repositories.
	case *remote.Repository:
		return true
	default:
		return false
	}
}
