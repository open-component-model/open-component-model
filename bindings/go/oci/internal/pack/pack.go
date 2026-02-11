// Package pack provides functionality for creating and managing OCI artifacts based on resources and blobs.
// It supports packing resources into OCI-compliant artifacts and pushing them to OCI registries.
package pack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	internaldigest "ocm.software/open-component-model/bindings/go/oci/internal/digest"
	"ocm.software/open-component-model/bindings/go/oci/internal/identity"
	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
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

	// EnforceGlobalAccess indicates if new resources should contain a global access regardless whether the
	// access is guaranteed to be valid or not
	EnforceGlobalAccess bool
}

// ArtifactBlob packs a [ociblob.ArtifactBlob] into an OCI Storage
func ArtifactBlob(ctx context.Context, storage content.Storage, b *ociblob.ArtifactBlob, opts Options) (ociImageSpecV1.Descriptor, error) {
	// Convert access to LocalBlob - this is a no-op if already the correct type
	localBlob := &v2.LocalBlob{}
	if err := opts.AccessScheme.Convert(b.GetAccess(), localBlob); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf(
			"failed to convert artifact access to local blob "+
				"(make sure this is a local blob access to prepare it for packing): %w", err)
	}
	return ResourceLocalBlob(ctx, storage, b, localBlob, opts)
}

func ResourceLocalBlob(ctx context.Context, storage content.Storage, b *ociblob.ArtifactBlob, access *v2.LocalBlob, opts Options) (desc ociImageSpecV1.Descriptor, err error) {
	mediaType := access.MediaType
	if mediaType == "" {
		if mtAware, ok := b.ReadOnlyBlob.(blob.MediaTypeAware); ok {
			mediaType, _ = mtAware.MediaType()
		}
	}

	switch mediaType {
	case layout.MediaTypeOCIImageLayoutTarV1, layout.MediaTypeOCIImageLayoutTarGzipV1:
		return ResourceLocalBlobOCILayout(ctx, storage, b, access, opts)
	default:
		return ResourceLocalBlobOCILayer(ctx, storage, b, access, opts)
	}
}

func ResourceLocalBlobOCILayer(ctx context.Context, storage content.Storage, b *ociblob.ArtifactBlob, access *v2.LocalBlob, opts Options) (ociImageSpecV1.Descriptor, error) {
	b, layer, err := PrepareArtifactBlobForOCI(b, ResourceBlobOCILayerOptions{
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

	global := backedByGlobalStore(storage) || opts.EnforceGlobalAccess

	if err := updateArtifactAccess(b.Artifact, access, layer, updateAccessOptions{opts, global}); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to update resource access: %w", err)
	}

	return layer, nil
}

func ResourceLocalBlobOCILayout(ctx context.Context, storage content.Storage, b *ociblob.ArtifactBlob, access *v2.LocalBlob, opts Options) (ociImageSpecV1.Descriptor, error) {
	index, err := tar.CopyOCILayoutWithIndex(ctx, storage, b, tar.CopyOCILayoutWithIndexOptions{
		CopyGraphOptions: opts.CopyGraphOptions,
		MutateParentFunc: func(idx *ociImageSpecV1.Descriptor) error {
			return identity.Adopt(idx, b.Artifact)
		},
	})
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to copy OCI layout: %w", err)
	}
	global := backedByGlobalStore(storage)
	if err := updateArtifactAccess(b.Artifact, access, index, updateAccessOptions{opts, global}); err != nil {
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

type OCILayerConvertableBlob interface {
	blob.SizeAware
	blob.MediaTypeAware
	blob.DigestAware
}

// PrepareArtifactBlobForOCI creates a new OCI layer descriptor for a OCILayerConvertableBlob.
// It ensures that the involved ArtifactBlob has the size and the digest set, so it can be uploaded to OCI registries,
// which need these fields for API interactions. If the incoming ArtifactBlob does not have a size or a digest,
// a new instance of ArtifactBlob having both fields set is created and returned. Otherwise, the initial ArtifactBlob is returned.
func PrepareArtifactBlobForOCI(b *ociblob.ArtifactBlob, opts ResourceBlobOCILayerOptions) (*ociblob.ArtifactBlob, ociImageSpecV1.Descriptor, error) {
	size := b.Size()

	var mediaType string
	if mediaTypeFromBlob, ok := b.MediaType(); ok {
		mediaType = mediaTypeFromBlob
	}
	if mediaType == "" {
		mediaType = opts.BlobMediaType
	}
	if mediaType == "" {
		return nil, ociImageSpecV1.Descriptor{}, errors.New("blob media type is unknown and cannot be packed into an oci blob")
	}

	var dig digest.Digest
	if blobDigest, ok := b.Digest(); ok {
		dig = digest.Digest(blobDigest)
		if err := dig.Validate(); err != nil {
			return nil, ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to validate blob digest: %w", err)
		}
	} else if len(opts.BlobDigest) > 0 {
		dig = opts.BlobDigest
		if err := dig.Validate(); err != nil {
			return nil, ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to validate blob digest: %w", err)
		}
	}

	var err error
	if size == blob.SizeUnknown || len(dig) == 0 {
		b, err = b.Buffer()
		if err != nil {
			return nil, ociImageSpecV1.Descriptor{}, fmt.Errorf("could not convert artifact blob from unbuffered to buffered: %w", err)
		}
		size = b.Size()
		digStr, _ := b.Digest()
		dig = digest.Digest(digStr)
	}

	layer := ociImageSpecV1.Descriptor{
		MediaType:   mediaType,
		Digest:      dig,
		Annotations: opts.BlobLayerAnnotations,
		Size:        size,
	}

	if err := identity.Adopt(&layer, b.Artifact); err != nil {
		return nil, ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to adopt descriptor based on resource: %w", err)
	}

	return b, layer, nil
}

// Blob handles the actual transfer of blob data to the OCI storage.
// It reads the blob content and pushes it to the storage using the provided descriptor.
// The function ensures proper cleanup of resources by closing the blob reader after the transfer.
func Blob(ctx context.Context, storage content.Pusher, b blob.ReadOnlyBlob, desc ociImageSpecV1.Descriptor) (err error) {
	layerData, err := b.ReadCloser()
	if err != nil {
		return fmt.Errorf("failed to get blob reader: %w", err)
	}
	defer func() {
		err = errors.Join(err, layerData.Close())
	}()

	if err := storage.Push(ctx, desc, io.NopCloser(layerData)); err != nil {
		return fmt.Errorf("failed to push layer: %w", err)
	}

	return nil
}

type updateAccessOptions struct {
	Options
	// BackedByGlobalStore indicates if the resource is backed by a global store.
	// This is used to determine if the resource access should be updated with a global reference.
	BackedByGlobalStore bool
}

// updateArtifactAccess updates the resource access with the new layer information.
// for setting a global access it uses the base reference given which must not already contain a digest.
// It is assumed that the base reference is a valid OCI reference in the form of
// <repository> or <repository>:<tag>, as the digest is added to pin the descriptor reference.
// Note that a global reference is only set if the resource is backed by a globally reachable store,
// as otherwise the reference would not be valid, e.g. in a CTF. See backedByGlobalStore for more information.
func updateArtifactAccess(artifact descriptor.Artifact, access *v2.LocalBlob, desc ociImageSpecV1.Descriptor, opts updateAccessOptions) error {
	if artifact == nil {
		return errors.New("artifact must not be nil")
	}

	typ := access.Type
	if typ.IsEmpty() {
		typ = runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion)
	}
	localBlob := &descriptor.LocalBlob{
		Type:           typ,
		LocalReference: desc.Digest.String(),
		MediaType:      desc.MediaType,
		ReferenceName:  access.ReferenceName,
	}

	if opts.BackedByGlobalStore {
		setGlobalAccess(opts.BaseReference, desc, localBlob)
	}

	// convert to apply the access
	access, err := descriptor.ConvertToV2LocalBlob(opts.AccessScheme, localBlob)
	if err != nil {
		return fmt.Errorf("failed to convert access to local blob: %w", err)
	}

	switch typed := artifact.(type) {
	case *descriptor.Source:
		typed.Access = access
	case *descriptor.Resource:
		typed.Access = access
		if typed.Digest == nil {
			typed.Digest = &descriptor.Digest{}
		}
		if err := internaldigest.Apply(typed.Digest, desc.Digest); err != nil {
			return fmt.Errorf("failed to apply digest to artifact: %w", err)
		}
	}

	return nil
}

// setGlobalAccess sets the global access for the given local blob.
// It creates an absolute reference to the blob in the global store
// and assigns it to the [*descriptor.LocalBlob.GlobalAccess] field of the local blob.
func setGlobalAccess(baseReference string, desc ociImageSpecV1.Descriptor, localBlob *descriptor.LocalBlob) {
	globalRef := fmt.Sprintf("%s@%s", baseReference, desc.Digest.String())
	if introspection.IsOCICompliantManifest(desc) {
		localBlob.GlobalAccess = &accessv1.OCIImage{
			// This is an absolute reference to the manifest in the global store.
			// It contains the base reference and the digest of the blob to form an absolute, pinned (by digest)
			// OCI reference. Because it is a manifest, we know it can be accessed as OCI Image.
			// If the OCI Image is not a OCI runtime Image, it counts as an OCI Artifact instead, but
			// for OCI references, we do not care about that.
			ImageReference: globalRef,
		}
	} else {
		// This is an absolute reference to the blob in the global store.
		// Instead of an image reference, we use the OCIImageLayer type here because we do not have an OCI
		// compliant manifest type. Instead we reference it purely as a layer.
		localBlob.GlobalAccess = &accessv1.OCIImageLayer{
			Reference: globalRef,
			MediaType: desc.MediaType,
			Digest:    desc.Digest,
			Size:      desc.Size,
		}
	}
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
