package constructor

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"

	"github.com/opencontainers/go-digest"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor/internal/log"
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// ErrShouldSkipConstruction is an error that indicates that the construction of a component should be skipped,
// e.g. because the component version already exists in the target repository.
var ErrShouldSkipConstruction = errors.New("should skip construction")

const AttributeDescriptor string = "constructor/descriptor"

type Constructor interface {
	// Construct processes a component constructor specification and creates the corresponding component descriptors.
	// It validates the constructor specification and processes each component in topological order.
	Construct(ctx context.Context) error

	// GetGraph returns the internal graph used during construction.
	// It can be used to inspect the relationships between components.
	// Construct must be called to ensure the graph is populated.
	// TODO: https://github.com/open-component-model/ocm-project/issues/736 this is temporary and needs to be replaced with proper abstraction
	GetGraph() *syncdag.SyncedDirectedAcyclicGraph[string]
}

type DefaultConstructor struct {
	componentDigestCacheMu sync.Mutex
	componentDigestCache   map[string]*descriptor.Digest
	discoverer             *syncdag.GraphDiscoverer[string, *ConstructorOrExternalComponent]
	constructor            *constructor.ComponentConstructor
	constructMutex         sync.Mutex
	opts                   Options
}

var _ Constructor = (*DefaultConstructor)(nil)

type componentVersionRepositoryWrapper struct {
	repository repository.ComponentVersionRepository
}

func (c componentVersionRepositoryWrapper) GetExternalRepository(ctx context.Context, _, _ string) (repository.ComponentVersionRepository, error) {
	return c.repository, nil
}

func RepositoryAsExternalComponentVersionRepositoryProvider(repo repository.ComponentVersionRepository) ExternalComponentRepositoryProvider {
	return componentVersionRepositoryWrapper{repository: repo}
}

var _ ExternalComponentRepositoryProvider = (*componentVersionRepositoryWrapper)(nil)

type ConstructorOrExternalComponent struct {
	ConstructorComponent *constructor.Component
	ExternalComponent    *DescriptorWithLocalBlobs
}

func (c *DefaultConstructor) Construct(ctx context.Context) error {
	c.constructMutex.Lock()
	defer c.constructMutex.Unlock()

	if err := c.discoverer.Graph().WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		if len(d.Vertices) > 0 {
			return fmt.Errorf("component constructor graph has already been constructed")
		}
		return nil
	}); err != nil {
		return err
	}

	logger := log.Base().With("operation", "constructComponent")

	if c.opts.ResourceInputMethodProvider == nil {
		logger.Debug("using default resource input method provider")
		c.opts.ResourceInputMethodProvider = DefaultInputMethodRegistry
	}
	if c.opts.SourceInputMethodProvider == nil {
		logger.Debug("using default source input method provider")
		c.opts.SourceInputMethodProvider = DefaultInputMethodRegistry
	}

	if len(c.constructor.Components) == 0 {
		return nil
	}

	err := c.discover(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover component constructor graph: %w", err)
	}
	err = c.construct(ctx)
	if err != nil {
		return fmt.Errorf("failed to constructComponent components from graph: %w", err)
	}

	return nil
}

func (c *DefaultConstructor) GetGraph() *syncdag.SyncedDirectedAcyclicGraph[string] {
	return c.discoverer.Graph()
}

func (c *DefaultConstructor) discover(ctx context.Context) error {
	slog.DebugContext(ctx, "starting discovery based on components in constructor")
	if err := c.discoverer.Discover(ctx); err != nil {
		return fmt.Errorf("failed to discover components: %w", err)
	}
	slog.DebugContext(ctx, "component reference discovery completed successfully")
	return nil
}

func (c *DefaultConstructor) construct(ctx context.Context) error {
	var (
		reversedGraph *dag.DirectedAcyclicGraph[string]
		err           error
	)
	if err = c.discoverer.Graph().WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		if reversedGraph, err = d.Reverse(); err != nil {
			return fmt.Errorf("failed to reverse graph: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	proc := processor{
		constructor: c,
		processedDescriptors: descriptors{
			graph: c.discoverer.Graph(),
		},
	}

	syncedReversedGraph := syncdag.ToSyncedGraph(reversedGraph)

	graphProcessor := syncdag.NewGraphProcessor(syncedReversedGraph, &syncdag.GraphProcessorOptions[string, *ConstructorOrExternalComponent]{
		Processor: &proc,
	})
	slog.DebugContext(ctx, "starting processing of discovered component graph")
	if err := graphProcessor.Process(ctx); err != nil {
		return fmt.Errorf("failed to process component constructor graph: %w", err)
	}
	slog.DebugContext(ctx, "component construction completed successfully")
	return nil
}

func NewDefaultConstructor(constructor *constructor.ComponentConstructor, opts Options) Constructor {
	discoverer := buildDiscoverer(constructor, opts)
	return &DefaultConstructor{
		componentDigestCache: make(map[string]*descriptor.Digest),
		constructor:          constructor,
		discoverer:           discoverer,
		opts:                 opts,
	}
}

// buildDiscoverer creates a graph discoverer for the component constructor.
// It sets up the roots of the graph based on the components in the constructor
func buildDiscoverer(componentConstructor *constructor.ComponentConstructor, opts Options) *syncdag.GraphDiscoverer[string, *ConstructorOrExternalComponent] {
	roots := make([]string, len(componentConstructor.Components))
	for index, comp := range componentConstructor.Components {
		roots[index] = comp.ToIdentity().String()
	}
	resAndDis := resolverAndDiscoverer{
		componentConstructor:                componentConstructor,
		externalComponentRepositoryProvider: opts.ExternalComponentRepositoryProvider,
		resolveExternalLocalBlobs:           opts.ExternalComponentVersionCopyPolicy == ExternalComponentVersionCopyPolicyCopyOrFail,
	}
	graphDiscoverer := syncdag.NewGraphDiscoverer(&syncdag.GraphDiscovererOptions[string, *ConstructorOrExternalComponent]{
		Roots:      roots,
		Resolver:   &resAndDis,
		Discoverer: &resAndDis,
	})
	return graphDiscoverer
}

// constructComponent creates a single component descriptor from a component specification.
// It handles the creation of the base descriptor, processes all resources concurrently,
// and adds the final component version to the target repository.
func (c *DefaultConstructor) constructComponent(ctx context.Context, component *constructor.Component, referencedComponents map[string]*descriptor.Descriptor) (*descriptor.Descriptor, error) {
	logger := log.Base().With("component", component.Name, "version", component.Version)
	desc := createBaseDescriptor(component)
	logger.Debug("created base descriptor")

	repo, err := c.opts.GetTargetRepository(ctx, component)
	if err != nil {
		return nil, fmt.Errorf("error getting target repository for component %q: %w", component.Name, err)
	}

	// decide how to handle existing component versions in the target repository
	// based on the configured conflict policy.
	conflictingDescriptor, err := ProcessConflictStrategy(ctx, repo, component, c.opts.ComponentVersionConflictPolicy)
	switch {
	case errors.Is(err, ErrShouldSkipConstruction):
		// skip construction if the policy is to skip existing versions, and return the existing descriptor
		return conflictingDescriptor, nil
	case err != nil:
		return nil, err
	}

	if err := c.processDescriptor(ctx, repo, component, desc, referencedComponents); err != nil {
		return nil, err
	}

	if err := repo.AddComponentVersion(ctx, desc); err != nil {
		return nil, fmt.Errorf("error adding component version to target: %w", err)
	}

	return desc, nil
}

// ProcessConflictStrategy checks for existing component versions in the target repository
// and applies the configured conflict resolution strategy.
// It returns an error if the policy is to abort and fail, or skips construction by returning ErrShouldSkipConstruction.
// If the policy is to replace, it logs a warning and does not return a possible existing descriptor for conflict resolution.
func ProcessConflictStrategy(ctx context.Context, repo TargetRepository, component *constructor.Component, policy ComponentVersionConflictPolicy) (*descriptor.Descriptor, error) {
	logger := log.Base().With("component", component.Name, "version", component.Version)
	switch policy {
	case ComponentVersionConflictAbortAndFail, ComponentVersionConflictSkip:
		logger.DebugContext(ctx, "checking for existing component version in target repository", "component", component.Name, "version", component.Version)
		switch desc, err := repo.GetComponentVersion(ctx, component.Name, component.Version); {
		case err == nil:
			if policy == ComponentVersionConflictAbortAndFail {
				return desc, fmt.Errorf("component version %q already exists in target repository", component.ToIdentity())
			}
			logger.WarnContext(ctx, "component version already exists in target repository, skipping construction", "component", component.Name, "version", component.Version)
			return desc, ErrShouldSkipConstruction
		case !errors.Is(err, repository.ErrNotFound):
			return nil, fmt.Errorf("error checking for existing component version in target repository: %w", err)
		default:
			logger.DebugContext(ctx, "no existing component version found in target repository, continuing with construction", "component", component.Name, "version", component.Version)
		}
	case ComponentVersionConflictReplace:
		logger.WarnContext(ctx, "REPLACING component version in target repository, old component version will no longer be available if it was present before.")
	}
	return nil, nil
}

// createBaseDescriptor initializes a new descriptor with the basic component metadata.
// It sets up the component name, version, labels, and provider information, and prepares
// empty slices for resources, sources, references, and repository contexts.
func createBaseDescriptor(component *constructor.Component) *descriptor.Descriptor {
	return constructor.ConvertToDescriptor(&constructor.ComponentConstructor{
		Components: []constructor.Component{*component},
	})
}

// processDescriptor handles the concurrent processing of all resources and sources in a component.
// It uses an errgroup to manage concurrent resource processing with a limit based on
// the number of available CPU cores.
func (c *DefaultConstructor) processDescriptor(
	ctx context.Context,
	targetRepo TargetRepository,
	component *constructor.Component,
	desc *descriptor.Descriptor,
	referencedComponents map[string]*descriptor.Descriptor,
) error {
	logger := log.Base().With("component", component.Name, "version", component.Version)
	logger.Debug("processing descriptor",
		"num_resources", len(component.Resources),
		"num_sources", len(component.Sources))

	eg, egctx := newConcurrencyGroup(ctx, c.opts.ConcurrencyLimit)
	var descLock sync.Mutex

	for i, resource := range component.Resources {
		resourceLogger := logger.With("resource", resource.ToIdentity())
		resourceLogger.Debug("processing resource")

		eg.Go(func() error {
			if c.opts.OnStartResourceConstruct != nil {
				if err := c.opts.OnStartResourceConstruct(egctx, &resource); err != nil {
					return fmt.Errorf("error starting resource construction for %q: %w", resource.ToIdentity(), err)
				}
			}
			res, err := c.processResource(egctx, targetRepo, &resource, component.Name, component.Version)
			if c.opts.OnEndResourceConstruct != nil {
				if err := c.opts.OnEndResourceConstruct(egctx, res, err); err != nil {
					return fmt.Errorf("error ending resource construction for %q: %w", resource.ToIdentity(), err)
				}
			}
			if err != nil {
				return fmt.Errorf("error processing resource %q at index %d: %w", resource.ToIdentity(), i, err)
			}
			descLock.Lock()
			defer descLock.Unlock()
			desc.Component.Resources[i] = *res
			resourceLogger.Debug("resource processed successfully")
			return nil
		})
	}

	for i, source := range component.Sources {
		sourceLogger := logger.With("source", source.ToIdentity())
		sourceLogger.Debug("processing source")

		eg.Go(func() error {
			if c.opts.OnStartSourceConstruct != nil {
				if err := c.opts.OnStartSourceConstruct(egctx, &source); err != nil {
					return fmt.Errorf("error starting source construction for %q: %w", source.ToIdentity(), err)
				}
			}
			src, err := c.processSource(egctx, targetRepo, &source, component.Name, component.Version)
			if c.opts.OnEndSourceConstruct != nil {
				if err := c.opts.OnEndSourceConstruct(egctx, src, err); err != nil {
					return fmt.Errorf("error ending source construction for %q: %w", source.ToIdentity(), err)
				}
			}
			if err != nil {
				return fmt.Errorf("error processing source %q at index %d: %w", source.ToIdentity(), i, err)
			}
			descLock.Lock()
			defer descLock.Unlock()
			desc.Component.Sources[i] = *src
			sourceLogger.Debug("source processed successfully")
			return nil
		})
	}

	for i, reference := range component.References {
		referenceLogger := logger.With("reference", reference.ToIdentity())
		referenceLogger.Debug("processing reference")

		eg.Go(func() error {
			if c.opts.OnStartReferenceConstruct != nil {
				if err := c.opts.OnStartReferenceConstruct(egctx, &reference); err != nil {
					return fmt.Errorf("error starting reference construction for %q: %w", reference.ToIdentity(), err)
				}
			}
			referencedComponent := referencedComponents[reference.ToIdentity().String()]
			ref, err := c.processReference(egctx, &reference, referencedComponent)
			if c.opts.OnEndReferenceConstruct != nil {
				if err := c.opts.OnEndReferenceConstruct(egctx, ref, err); err != nil {
					return fmt.Errorf("error ending reference construction for %q: %w", ref.ToIdentity(), err)
				}
			}
			if err != nil {
				return fmt.Errorf("error processing reference %q at index %d: %w", ref.ToIdentity(), i, err)
			}
			descLock.Lock()
			defer descLock.Unlock()
			desc.Component.References[i] = *ref
			referenceLogger.Debug("reference processed successfully")
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("error constructing component: %w", err)
	}

	logger.Debug("descriptor processing completed successfully")
	return nil
}

// processResource handles the processing of a single resource, including both input and non-input cases.
// It ensures thread-safe access to the descriptor when updating resource information
// and validates that the processed resource has proper access information.
func (c *DefaultConstructor) processResource(ctx context.Context, targetRepo TargetRepository, resource *constructor.Resource, component, version string) (*descriptor.Resource, error) {
	logger := log.Base().With(
		"component", component,
		"version", version,
		"resource", resource.ToIdentity(),
	)
	logger.Debug("processing resource")

	var res *descriptor.Resource
	var err error

	switch {
	case resource.HasInput():
		if resource.CopyPolicy != "" && resource.CopyPolicy != constructor.CopyPolicyByValue {
			return nil, fmt.Errorf("invalid copy policy %q for resource %q, "+
				"due to an input specification an empty policy or %q is expected", resource.CopyPolicy, resource.ToIdentity(), constructor.CopyPolicyByValue)
		}
		logger.Debug("processing resource with input method")
		res, err = c.processResourceWithInput(ctx, targetRepo, resource, component, version)
	case resource.HasAccess():
		if resource.Relation == "" {
			logger.Debug("defaulting resource relation to external as resource is accessed by reference")
			resource.Relation = constructor.ExternalRelation
		}
		if resource.Version == "" {
			logger.Debug("defaulting resource version to component version as no resource version was set")
			resource.Version = version
		}
		if byValue := resource.CopyPolicy == constructor.CopyPolicyByValue; byValue {
			logger.Debug("processing resource by value")
			res, err = c.processResourceByValue(ctx, targetRepo, resource, component, version)
		} else {
			logger.Debug("processing resource with existing access")
			res = constructor.ConvertToDescriptorResource(resource)

			if c.opts.ResourceDigestProcessorProvider != nil {
				var digestProcessor ResourceDigestProcessor
				if digestProcessor, err = c.opts.GetDigestProcessor(ctx, res); err == nil {
					logger.Debug("processing resource digest")
					var creds map[string]string
					if c.opts.Resolver != nil {
						if identity, err := digestProcessor.GetResourceDigestProcessorCredentialConsumerIdentity(ctx, res); err == nil {
							if creds, err = resolveCredentials(ctx, c.opts.Resolver, identity); err != nil {
								return nil, fmt.Errorf("error resolving credentials for resource digest processor: %w", err)
							}
						} else {
							logger.Debug("no credential consumer identity found for resource digest processor, skipping credential resolution")
						}
					}
					if res, err = digestProcessor.ProcessResourceDigest(ctx, res, creds); err != nil {
						return nil, fmt.Errorf("error processing resource %q with digest processor: %w", resource.ToIdentity(), err)
					}
				}
			}
		}
	default:
		return nil, fmt.Errorf("resource %q has no access type and no input method", resource.ToIdentity())
	}

	if err != nil {
		return nil, fmt.Errorf("error processing resource %q: %w", resource.ToIdentity(), err)
	}

	if res.Access == nil {
		return nil, fmt.Errorf("after the input method was processed, no access was present in the resource. This is likely a problem in the input method")
	}

	logger.Debug("resource processed successfully")

	return res, nil
}

func (c *DefaultConstructor) processResourceByValue(ctx context.Context, targetRepo TargetRepository, resource *constructor.Resource, component, version string) (*descriptor.Resource, error) {
	repo, err := c.opts.GetResourceRepository(ctx, resource)
	if err != nil {
		return nil, err
	}

	converted := constructor.ConvertToDescriptorResource(resource)

	// best effort to resolve credentials for by value resource download.
	// if no identity is resolved, we assume resolution is simply skipped.
	var creds map[string]string
	if identity, err := repo.GetResourceCredentialConsumerIdentity(ctx, resource); err == nil {
		if creds, err = resolveCredentials(ctx, c.opts.Resolver, identity); err != nil {
			return nil, fmt.Errorf("error resolving credentials for resource by-value processing %w", err)
		}
	}

	data, err := repo.DownloadResource(ctx, converted, creds)
	if err != nil {
		return nil, fmt.Errorf("error downloading resource: %w", err)
	}
	return addColocatedResourceLocalBlob(ctx, targetRepo, component, version, resource, data)
}

func (c *DefaultConstructor) processSource(ctx context.Context, targetRepo TargetRepository, src *constructor.Source, component, version string) (*descriptor.Source, error) {
	logger := log.Base().With(
		"component", component,
		"version", version,
		"source", src.ToIdentity(),
	)
	logger.Debug("processing source")

	var res *descriptor.Source
	var err error

	if src.HasInput() {
		logger.Debug("processing source with input method")
		res, err = c.processSourceWithInput(ctx, targetRepo, src, component, version)
	} else {
		logger.Debug("processing source with existing access")
		res = constructor.ConvertToDescriptorSource(src)
	}

	if err != nil {
		return nil, fmt.Errorf("error processing source %q: %w", src.ToIdentity(), err)
	}

	if res.Access == nil {
		return nil, fmt.Errorf("after the input method was processed, no access was present in the source. This is likely a problem in the input method")
	}

	logger.Debug("source processed successfully")
	return res, nil
}

// processSourceWithInput handles the specific case of processing a source that has an input method.
// It looks up the appropriate input method from the registry and processes the source
// using the found method.
func (c *DefaultConstructor) processSourceWithInput(ctx context.Context, targetRepo TargetRepository, src *constructor.Source, component, version string) (*descriptor.Source, error) {
	method, err := c.opts.GetSourceInputMethod(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("no input method resolvable for input specification of type %q: %w", src.Input.GetType(), err)
	}

	// best effort to resolve credentials for the input method.
	// if no identity is resolved, we assume resolution is simply skipped.
	var creds map[string]string
	if identity, err := method.GetSourceCredentialConsumerIdentity(ctx, src); err == nil {
		if creds, err = resolveCredentials(ctx, c.opts.Resolver, identity); err != nil {
			return nil, fmt.Errorf("error resolving credentials for source input method: %w", err)
		}
	}

	result, err := method.ProcessSource(ctx, src, creds)
	if err != nil {
		return nil, fmt.Errorf("error getting blob from input method: %w", err)
	}

	var processedSource *descriptor.Source

	if result.ProcessedBlobData != nil {
		processedSource, err = addColocatedSourceLocalBlob(ctx, targetRepo, component, version, src, result.ProcessedBlobData)
	} else if result.ProcessedSource != nil {
		processedSource = result.ProcessedSource
	}

	if err != nil {
		return nil, fmt.Errorf("error adding source %q to target repository: %w", src.ToIdentity(), err)
	}
	if processedSource == nil {
		return nil, fmt.Errorf("input method for source %q did not return a processed source or blob data", src.ToIdentity())
	}

	return processedSource, nil
}

// processResourceWithInput handles the specific case of processing a resource that has an input method.
// It looks up the appropriate input method from the registry and processes the resource
// using the found method.
func (c *DefaultConstructor) processResourceWithInput(ctx context.Context, targetRepo TargetRepository, resource *constructor.Resource, component, version string) (*descriptor.Resource, error) {
	method, err := c.opts.GetResourceInputMethod(ctx, resource)
	if err != nil {
		return nil, fmt.Errorf("no input method resolvable for input specification of type %q: %w", resource.Input.GetType(), err)
	}

	// best effort to resolve credentials for the input method.
	// if no identity is resolved, we assume resolution is simply skipped.
	var creds map[string]string
	if identity, err := method.GetResourceCredentialConsumerIdentity(ctx, resource); err == nil {
		if creds, err = resolveCredentials(ctx, c.opts.Resolver, identity); err != nil {
			return nil, fmt.Errorf("error resolving credentials for resource input method: %w", err)
		}
	}

	result, err := method.ProcessResource(ctx, resource, creds)
	if err != nil {
		return nil, fmt.Errorf("error getting blob from input method: %w", err)
	}

	var processedResource *descriptor.Resource

	if result.ProcessedBlobData != nil {
		processedResource, err = addColocatedResourceLocalBlob(ctx, targetRepo, component, version, resource, result.ProcessedBlobData)
	} else if result.ProcessedResource != nil {
		processedResource = result.ProcessedResource
	}

	if err != nil {
		return nil, fmt.Errorf("error adding resource %q to target repository: %w", resource.ToIdentity(), err)
	}
	if processedResource == nil {
		return nil, fmt.Errorf("input method for resource %q did not return a processed resource or blob data", resource.ToIdentity())
	}

	return processedResource, nil
}

// processReference processes a component reference by calculating its digest and converting it to a descriptor reference.
func (c *DefaultConstructor) processReference(ctx context.Context, reference *constructor.Reference, referencedComponent *descriptor.Descriptor) (*descriptor.Reference, error) {
	logger := log.Base().With(
		"ref", reference.ToIdentity(),
	)
	logger.Debug("processing reference")

	referencedComponentDigest, err := c.getComponentDigest(ctx, reference.ToIdentity().String(), referencedComponent)
	if err != nil {
		return nil, fmt.Errorf("error getting digest for referenced component %q: %w", reference.ToIdentity(), err)
	}

	ref := constructor.ConvertToDescriptorReference(reference)
	ref.Digest = *referencedComponentDigest

	logger.Debug("reference processed successfully")
	return ref, nil
}

// getComponentDigest tries to get the digest for a particular component from
// cache. If there is no cached digest for that particular component, it
// calculates the digest and stores it in the cache.
// We want this operation to be atomar, to avoid concurrent calls for the
// same component to have cache misses. That would lead to multiple
// calculations of the same digest.
func (c *DefaultConstructor) getComponentDigest(ctx context.Context, componentIdentity string, referencedComponent *descriptor.Descriptor) (*descriptor.Digest, error) {
	c.componentDigestCacheMu.Lock()
	defer c.componentDigestCacheMu.Unlock()

	if componentDigest, cached := c.componentDigestCache[componentIdentity]; cached {
		slog.DebugContext(ctx, "component digest found in cache", "component", componentIdentity)
		return componentDigest, nil
	}
	slog.DebugContext(ctx, "component digest not found in cache", "component", componentIdentity)

	componentDigest, err := calculateDigest(referencedComponent)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate digest: %w", err)
	}
	c.componentDigestCache[componentIdentity] = componentDigest
	return componentDigest, nil
}

func calculateDigest(component *descriptor.Descriptor) (*descriptor.Digest, error) {
	normalisedData, err := normalisation.Normalisations.Normalise(component, v4alpha1.Algorithm)
	if err != nil {
		return nil, fmt.Errorf("error normalising descriptor %s: %w", component.Component.ToIdentity().String(), err)
	}

	return &descriptor.Digest{
		HashAlgorithm:          crypto.SHA256.String(),
		NormalisationAlgorithm: v4alpha1.Algorithm,
		Value:                  digest.SHA256.FromBytes(normalisedData).Encoded(),
	}, nil
}

// addColocatedResourceLocalBlob adds a local blob to the component version repository and defaults fields relevant
// to declare the spec.LocalRelation to the component version as well as default the resource version and media type:
//
//  1. If no resource relation is set, it defaults to constructor.LocalRelation because the resource is then located
//     locally alongside the component
//  2. If the media type is available it is used for the local blob specification.
//
// The resource is expected to be a local resource so the access that is created is always a local blob.
func addColocatedResourceLocalBlob(
	ctx context.Context,
	repo TargetRepository,
	component, version string,
	resource *constructor.Resource,
	data blob.ReadOnlyBlob,
) (processed *descriptor.Resource, err error) {
	localBlob := &v2.LocalBlob{}

	if mediaTypeAware, ok := data.(blob.MediaTypeAware); ok {
		localBlob.MediaType, _ = mediaTypeAware.MediaType()
	}
	if localBlob.MediaType == "" {
		// If the media type is not set, default to application/octet-stream, which is a common fallback
		// for binary data. This is a safe default for local blobs that do not have a specific media type,
		// as it is never truly "wrong".
		localBlob.MediaType = "application/octet-stream"
	}

	// if the resource doesn't have any information about its relation to the component
	// default to a local resource. This means that if not specified, we assume the resource is co-created
	// with the component and is not an external resource.
	if resource.Relation == "" {
		resource.Relation = constructor.LocalRelation
	}

	// if the resource doesn't have any information about its version,
	// default to the component version. This is useful for resources that are colocated
	// and constructed alongside the component.
	if resource.Version == "" {
		resource.Version = version
	}

	descResource := constructor.ConvertToDescriptorResource(resource)
	descResource.Access = localBlob

	uploaded, err := repo.AddLocalResource(ctx, component, version, descResource, data)
	if err != nil {
		return nil, fmt.Errorf("error adding local resource %q based on input type %q as local resource to component %q : %w", resource.ToIdentity(), resource.Input.GetType(), component, err)
	}

	return uploaded, nil
}

func addColocatedSourceLocalBlob(
	ctx context.Context,
	repo TargetRepository,
	component, version string,
	source *constructor.Source,
	data blob.ReadOnlyBlob,
) (processed *descriptor.Source, err error) {
	localBlob := &descriptor.LocalBlob{}

	if mediaTypeAware, ok := data.(blob.MediaTypeAware); ok {
		localBlob.MediaType, _ = mediaTypeAware.MediaType()
	}

	// if the source doesn't have any information about its version,
	// default to the component version.
	if source.Version == "" {
		source.Version = version
	}

	descSource := constructor.ConvertToDescriptorSource(source)
	descSource.Access = localBlob

	uploaded, err := repo.AddLocalSource(ctx, component, version, descSource, data)
	if err != nil {
		return nil, fmt.Errorf("error adding local source %q based on input type %q as local resource to component %q : %w", source.ToIdentity(), source.Input.GetType(), component, err)
	}

	return uploaded, nil
}

func newConcurrencyGroup(ctx context.Context, limit int) (*errgroup.Group, context.Context) {
	logger := log.Base().With("operation", "new_concurrency_group")

	eg, egctx := errgroup.WithContext(ctx)

	if limit > 0 {
		logger.Debug("setting custom concurrency limit", "limit", limit)
		eg.SetLimit(limit)
	} else {
		cores := runtime.NumCPU()
		logger.Debug("using CPU core count as concurrency limit", "cores", cores)
		eg.SetLimit(cores)
	}
	return eg, egctx
}

// resolveCredentials attempts to resolve credentials for a given credential consumerIdentity.
// It returns the resolved credentials and any error that occurred during resolution.
// If no credentials are needed or available, it returns nil credentials and no error.
func resolveCredentials(ctx context.Context, provider credentials.Resolver, consumerIdentity ocmruntime.Identity) (map[string]string, error) {
	logger := log.Base().With("identity", consumerIdentity)

	if provider == nil {
		logger.DebugContext(ctx, "no credential provider configured, skipping credential resolution")
		return nil, nil
	}

	if consumerIdentity == nil {
		logger.DebugContext(ctx, "no credential consumer identity found, proceeding without credentials")
		return nil, nil
	}

	creds, err := provider.Resolve(ctx, consumerIdentity)
	if errors.Is(err, credentials.ErrNotFound) {
		logger.DebugContext(ctx, "no credentials found for consumer identity, proceeding without credentials")
		return nil, nil
	}

	return creds, err
}
