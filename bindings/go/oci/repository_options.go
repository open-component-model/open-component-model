package oci

import (
	"context"
	"fmt"
	"log/slog"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocmoci "ocm.software/open-component-model/bindings/go/oci/access"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// LocalResourceLayerCreationMode defines how local resources should be accessed in the repository.
type LocalResourceLayerCreationMode string

const (
	// LocalResourceCreationModeLocalBlobWithNestedGlobalAccess creates a local blob access for resources.
	// It also embeds the global access information in the local blob.
	LocalResourceCreationModeLocalBlobWithNestedGlobalAccess LocalResourceLayerCreationMode = "localBlob"
	// LocalResourceCreationModeOCIImageLayer creates an OCI image layer access for resources.
	// This mode is used when the resource is embedded without a local blob (only global access)
	LocalResourceCreationModeOCIImageLayer LocalResourceLayerCreationMode = "ociImageLayer"
)

// RepositoryOptions defines the options for creating a new Repository.
type RepositoryOptions struct {
	// Scheme is the runtime scheme used for type conversion.
	// If not provided, a new scheme will be created with default registrations.
	Scheme *runtime.Scheme
	// LocalBlobMemory is used to temporarily store local blobs until they are added to a component version.
	// If not provided, a new memory will be created.
	LocalLayerBlobMemory    LocalBlobMemory
	LocalManifestBlobMemory LocalBlobMemory
	// Resolver resolves component version references to OCI stores.
	// This is required and must be provided.
	Resolver Resolver

	// Creator is the creator of new Component Versions.
	// See AnnotationOCMCreator for details
	Creator string

	// CopyOptions are the options for copying resources between sources and targets
	ResourceCopyOptions *oras.CopyOptions

	// LocalResourceCreationMode determines how resources should be accessed in the repository.
	// Defaults to LocalResourceCreationModeLocalBlobWithNestedGlobalAccess.
	LocalResourceCreationMode LocalResourceLayerCreationMode
}

// RepositoryOption is a function that modifies RepositoryOptions.
type RepositoryOption func(*RepositoryOptions)

// WithScheme sets the runtime scheme for the repository.
func WithScheme(scheme *runtime.Scheme) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.Scheme = scheme
	}
}

// WithLocalLayerBlobMemory sets the local blob memory for the repository.
func WithLocalLayerBlobMemory(memory LocalBlobMemory) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.LocalLayerBlobMemory = memory
	}
}

// WithLocalManifestBlobMemory sets the local blob memory for the repository.
func WithLocalManifestBlobMemory(memory LocalBlobMemory) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.LocalManifestBlobMemory = memory
	}
}

// WithResolver sets the resolver for the repository.
func WithResolver(resolver Resolver) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.Resolver = resolver
	}
}

// WithLocalResourceCreationMode sets the access mode for resources in the repository.
func WithLocalResourceCreationMode(mode LocalResourceLayerCreationMode) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.LocalResourceCreationMode = mode
	}
}

// NewRepository creates a new Repository instance with the given options.
func NewRepository(opts ...RepositoryOption) (*Repository, error) {
	options := &RepositoryOptions{
		LocalResourceCreationMode: LocalResourceCreationModeLocalBlobWithNestedGlobalAccess, // Set default access mode
	}
	for _, opt := range opts {
		opt(options)
	}

	if options.Resolver == nil {
		return nil, fmt.Errorf("resolver is required")
	}

	if options.Scheme == nil {
		options.Scheme = runtime.NewScheme()
		ocmoci.MustAddToScheme(options.Scheme)
		v2.MustAddToScheme(options.Scheme)
	}

	if options.LocalLayerBlobMemory == nil {
		options.LocalLayerBlobMemory = NewInMemoryLocalBlobMemory()
	}
	if options.LocalManifestBlobMemory == nil {
		options.LocalManifestBlobMemory = NewInMemoryLocalBlobMemory()
	}

	if options.Creator == "" {
		options.Creator = "Open Component Model Go Reference Library"
	}

	if options.ResourceCopyOptions == nil {
		options.ResourceCopyOptions = &oras.CopyOptions{
			CopyGraphOptions: oras.CopyGraphOptions{
				Concurrency: 8,
				PreCopy: func(ctx context.Context, desc ociImageSpecV1.Descriptor) error {
					slog.DebugContext(ctx, "copying", descriptorLogAttr(desc))
					return nil
				},
				PostCopy: func(ctx context.Context, desc ociImageSpecV1.Descriptor) error {
					slog.InfoContext(ctx, "copied", descriptorLogAttr(desc))
					return nil
				},
				OnCopySkipped: func(ctx context.Context, desc ociImageSpecV1.Descriptor) error {
					slog.DebugContext(ctx, "skipped", descriptorLogAttr(desc))
					return nil
				},
			},
		}
	}

	return &Repository{
		scheme:                    options.Scheme,
		localLayerBlobMemory:      options.LocalLayerBlobMemory,
		localManifestBlobMemory:   options.LocalManifestBlobMemory,
		resolver:                  options.Resolver,
		creatorAnnotation:         options.Creator,
		resourceCopyOptions:       *options.ResourceCopyOptions,
		localResourceCreationMode: options.LocalResourceCreationMode,
	}, nil
}
