package oci

import (
	"fmt"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocmoci "ocm.software/open-component-model/bindings/go/oci/access"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// RepositoryOptions defines the options for creating a new Repository.
type RepositoryOptions struct {
	// Scheme is the runtime scheme used for type conversion.
	// If not provided, a new scheme will be created with default registrations.
	Scheme *runtime.Scheme
	// LocalBlobMemory is used to temporarily store local blobs until they are added to a component version.
	// If not provided, a new memory will be created.
	LocalBlobMemory LocalBlobMemory
	// Resolver resolves component version references to OCI stores.
	// This is required and must be provided.
	Resolver Resolver

	// Creator is the creator of new Component Versions.
	// See AnnotationOCMCreator for details
	Creator string
}

// RepositoryOption is a function that modifies RepositoryOptions.
type RepositoryOption func(*RepositoryOptions)

// WithScheme sets the runtime scheme for the repository.
func WithScheme(scheme *runtime.Scheme) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.Scheme = scheme
	}
}

// WithLocalBlobMemory sets the local blob memory for the repository.
func WithLocalBlobMemory(memory LocalBlobMemory) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.LocalBlobMemory = memory
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

	if options.LocalBlobMemory == nil {
		options.LocalBlobMemory = NewInMemoryLocalBlobMemory()
	}

	if options.Creator == "" {
		options.Creator = "Open Component Model Go Reference Library"
	}

	return &Repository{
		scheme:          options.Scheme,
		localBlobMemory: options.LocalBlobMemory,
		resolver:        options.Resolver,
	}, nil
}
