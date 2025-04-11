package oci

import (
	"context"
	"fmt"
	"log/slog"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/internal/log"
	"ocm.software/open-component-model/bindings/go/oci/internal/memory"
	ocmoci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// RepositoryOptions defines the options for creating a new Repository.
type RepositoryOptions struct {
	// Scheme is the runtime scheme used for type conversion.
	// If not provided, a new scheme will be created with default registrations.
	Scheme *runtime.Scheme
	// LocalDescriptorMemory is used to temporarily store local blobs until they are added to a component version.
	// If not provided, a new memory will be created.
	LocalManifestMemory memory.LocalDescriptorMemory
	// Resolver resolves component version references to OCI stores.
	// This is required and must be provided.
	Resolver Resolver

	// Creator is the creator of new Component Versions.
	// See AnnotationOCMCreator for details
	Creator string

	// CopyOptions are the options for copying resources between sources and targets
	ResourceCopyOptions *oras.CopyOptions
}

// RepositoryOption is a function that modifies RepositoryOptions.
type RepositoryOption func(*RepositoryOptions)

// WithScheme sets the runtime scheme for the repository.
func WithScheme(scheme *runtime.Scheme) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.Scheme = scheme
	}
}

// WithLocalManifestBlobMemory sets the local blob memory for the repository.
func WithLocalManifestBlobMemory(memory memory.LocalDescriptorMemory) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.LocalManifestMemory = memory
	}
}

// WithCreator sets the creator for the repository.
func WithCreator(creator string) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.Creator = creator
	}
}

// WithResolver sets the resolver for the repository.
func WithResolver(resolver Resolver) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.Resolver = resolver
	}
}

// NewRepository creates a new Repository instance with the given options.
func NewRepository(opts ...RepositoryOption) (*Repository, error) {
	options := &RepositoryOptions{}
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

	if options.LocalManifestMemory == nil {
		options.LocalManifestMemory = memory.NewInMemory()
	}

	if options.Creator == "" {
		options.Creator = "Open Component Model Go Reference Library"
	}

	if options.ResourceCopyOptions == nil {
		options.ResourceCopyOptions = &oras.CopyOptions{
			CopyGraphOptions: oras.CopyGraphOptions{
				Concurrency: 8,
				PreCopy: func(ctx context.Context, desc ociImageSpecV1.Descriptor) error {
					slog.DebugContext(ctx, "copying", log.DescriptorLogAttr(desc))
					return nil
				},
				PostCopy: func(ctx context.Context, desc ociImageSpecV1.Descriptor) error {
					slog.InfoContext(ctx, "copied", log.DescriptorLogAttr(desc))
					return nil
				},
				OnCopySkipped: func(ctx context.Context, desc ociImageSpecV1.Descriptor) error {
					slog.DebugContext(ctx, "skipped", log.DescriptorLogAttr(desc))
					return nil
				},
			},
		}
	}

	return &Repository{
		scheme:              options.Scheme,
		localManifestMemory: options.LocalManifestMemory,
		resolver:            options.Resolver,
		creatorAnnotation:   options.Creator,
		resourceCopyOptions: *options.ResourceCopyOptions,
	}, nil
}
