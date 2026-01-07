package oci

import (
	"context"
	"fmt"
	"log/slog"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	"ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	"ocm.software/open-component-model/bindings/go/oci/internal/log"
	ocmoci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	"ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var DefaultRepositoryScheme = runtime.NewScheme()

func init() {
	ocmoci.MustAddToScheme(DefaultRepositoryScheme)
	v2.MustAddToScheme(DefaultRepositoryScheme)
}

// RepositoryOptions defines the options for creating a new Repository.
type RepositoryOptions struct {
	// Scheme is the runtime scheme used for type conversion.
	// If not provided, a new scheme will be created with default registrations.
	Scheme *runtime.Scheme
	// LocalManifestCache is used to temporarily store local blobs until they are added to a component version.
	// If not provided, a new memory based cache will be created.
	LocalManifestCache cache.OCIDescriptorCache
	// LocalLayerCache is used to temporarily store local blobs until they are added to a component version.
	// If not provided, a new memory based cache will be created.
	LocalLayerCache cache.OCIDescriptorCache
	// Resolver resolves component version references to OCI stores.
	// This is required and must be provided.
	Resolver Resolver

	// Creator is the creator of new Component Versions.
	// See AnnotationOCMCreator for details
	Creator string

	// CopyOptions are the options for copying resources between sources and targets
	ResourceCopyOptions *oras.CopyOptions

	// ReferrerTrackingPolicy defines how OCI referrers are used to track component versions.
	ReferrerTrackingPolicy ReferrerTrackingPolicy

	// Logger is the logger to use for OCI operations.
	// If not provided, slog.Default() will be used.
	Logger *slog.Logger

	// TempDir is the default temporary filesystem folder for any temporary cached data
	TempDir string

	// DescriptorEncodingMediaType is the media type of the descriptor encoding used for component versions.
	DescriptorEncodingMediaType string

	// DescriptorUnmarshalFunc is used to unmarshal descriptors from OCI stores.
	// If not provided, DefaultDescriptorUnmarshalFunc will be used.
	DescriptorUnmarshalFunc descriptor.UnmarshalFunc
}

// ReferrerTrackingPolicy defines how OCI referrers are used in the repository.
// see https://github.com/opencontainers/distribution-spec/blob/main/spec.md#listing-referrers
type ReferrerTrackingPolicy int

const (
	// ReferrerTrackingPolicyNone means that no referrers are tracked.
	// This is the default policy and means that no referrers are used to track component versions.
	//
	// This is generally less accurate and efficient, but will work with any OCI repository that only
	// contains OCM component versions as tags.
	ReferrerTrackingPolicyNone ReferrerTrackingPolicy = iota
	// ReferrerTrackingPolicyByIndexAndSubject
	// means that added manifests are tracked using a static index and referencing that in the subject.
	//
	// If the index / subject can be stored via OCI referrers API, it will be used.
	//
	// If not, a new manifest with the index will be created and tagged with the digest of the index.
	// see https://github.com/opencontainers/distribution-spec/blob/main/spec.md#backwards-compatibility
	// for details on backwards-compatibility and behavior.
	ReferrerTrackingPolicyByIndexAndSubject ReferrerTrackingPolicy = iota
)

// RepositoryOption is a function that modifies RepositoryOptions.
type RepositoryOption func(*RepositoryOptions)

// WithScheme sets the runtime scheme for the repository.
func WithScheme(scheme *runtime.Scheme) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.Scheme = scheme
	}
}

// WithManifestCache sets the local oci descriptor cache for manifests.
func WithManifestCache(memory cache.OCIDescriptorCache) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.LocalManifestCache = memory
	}
}

// WithLayerCache sets the local oci descriptor cache for the layers.
func WithLayerCache(memory cache.OCIDescriptorCache) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.LocalLayerCache = memory
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

// WithReferrerTrackingPolicy sets the ReferrerTrackingPolicy for the repository.
func WithReferrerTrackingPolicy(policy ReferrerTrackingPolicy) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.ReferrerTrackingPolicy = policy
	}
}

// WithDescriptorEncodingMediaType sets the media type used for encoding component versions.
func WithDescriptorEncodingMediaType(mediaType string) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.DescriptorEncodingMediaType = mediaType
	}
}

// WithLogger sets the logger for the repository.
func WithLogger(logger *slog.Logger) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.Logger = logger
	}
}

// WithTempDir sets the temporary directory for the repository to use for caching data.
func WithTempDir(tempDir string) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.TempDir = tempDir
	}
}

// WithDescriptorUnmarshalFunc sets the function used to unmarshal a descriptor in the RepositoryOptions.
func WithDescriptorUnmarshalFunc(unmarshal descriptor.UnmarshalFunc) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.DescriptorUnmarshalFunc = unmarshal
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
		options.Scheme = DefaultRepositoryScheme
	}

	if options.LocalManifestCache == nil {
		options.LocalManifestCache = inmemory.New()
	}
	if options.LocalLayerCache == nil {
		options.LocalLayerCache = inmemory.New()
	}

	if options.Creator == "" {
		options.Creator = "Open Component Model Go Reference Library"
	}

	if options.Logger == nil {
		options.Logger = slog.Default()
	}

	if options.DescriptorEncodingMediaType == "" {
		options.DescriptorEncodingMediaType = descriptor.MediaTypeComponentDescriptorJSON
	}

	if options.DescriptorUnmarshalFunc == nil {
		options.DescriptorUnmarshalFunc = descriptor.DefaultDescriptorUnmarshalFunc
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
		scheme:                      options.Scheme,
		localArtifactManifestCache:  options.LocalManifestCache,
		localArtifactLayerCache:     options.LocalLayerCache,
		resolver:                    options.Resolver,
		creatorAnnotation:           options.Creator,
		resourceCopyOptions:         *options.ResourceCopyOptions,
		referrerTrackingPolicy:      options.ReferrerTrackingPolicy,
		descriptorEncodingMediaType: options.DescriptorEncodingMediaType,
		logger:                      options.Logger,
		unmarshalDescriptorFunc:     options.DescriptorUnmarshalFunc,
	}, nil
}
