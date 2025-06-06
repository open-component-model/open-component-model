package constructor

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor/internal/log"
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

type Constructor interface {
	// Construct processes a component constructor specification and creates the corresponding component descriptors.
	// It validates the constructor specification and processes each component in sequence.
	Construct(ctx context.Context, constructor *constructor.ComponentConstructor) ([]*descriptor.Descriptor, error)
}

// ConstructDefault is a convenience function that creates a new default DefaultConstructor and calls its Constructor.Construct method.
func ConstructDefault(ctx context.Context, constructor *constructor.ComponentConstructor, opts Options) ([]*descriptor.Descriptor, error) {
	return NewDefaultConstructor(opts).Construct(ctx, constructor)
}

type DefaultConstructor struct {
	opts Options
}

func (c *DefaultConstructor) Construct(ctx context.Context, constructor *constructor.ComponentConstructor) ([]*descriptor.Descriptor, error) {
	logger := log.Base().With("operation", "construct")

	if err := constructor.Validate(); err != nil {
		return nil, err
	}
	if c.opts.ResourceInputMethodProvider == nil {
		logger.Debug("using default resource input method provider")
		c.opts.ResourceInputMethodProvider = DefaultInputMethodRegistry
	}
	if c.opts.SourceInputMethodProvider == nil {
		logger.Debug("using default source input method provider")
		c.opts.SourceInputMethodProvider = DefaultInputMethodRegistry
	}

	descriptors := make([]*descriptor.Descriptor, len(constructor.Components))
	var descLock sync.Mutex

	eg, egctx := newConcurrencyGroup(ctx, c.opts.ConcurrencyLimit)
	logger.Debug("created concurrency group", "limit", c.opts.ConcurrencyLimit)

	for i, component := range constructor.Components {
		componentLogger := logger.With("component", component.Name, "version", component.Version)
		componentLogger.Info("constructing component")

		eg.Go(func() error {
			desc, err := c.construct(egctx, &component)
			if err != nil {
				return err
			}

			descLock.Lock()
			defer descLock.Unlock()
			descriptors[i] = desc
			componentLogger.Debug("component constructed successfully")

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("error constructing components: %w", err)
	}

	logger.Info("component construction completed successfully", "num_components", len(descriptors))
	return descriptors, nil
}

func NewDefaultConstructor(opts Options) Constructor {
	return &DefaultConstructor{
		opts: opts,
	}
}

// construct creates a single component descriptor from a component specification.
// It handles the creation of the base descriptor, processes all resources concurrently,
// and adds the final component version to the target repository.
func (c *DefaultConstructor) construct(ctx context.Context, component *constructor.Component) (*descriptor.Descriptor, error) {
	logger := log.Base().With("component", component.Name, "version", component.Version)

	if err := component.Validate(); err != nil {
		return nil, err
	}

	desc := createBaseDescriptor(component)
	logger.Debug("created base descriptor")

	repo, err := c.opts.GetTargetRepository(ctx, component)
	if err != nil {
		return nil, fmt.Errorf("error getting target repository for component %q: %w", component.Name, err)
	}

	switch c.opts.ComponentVersionConflictPolicy {
	case ComponentVersionConflictAbortAndFail, ComponentVersionConflictSkip:
		logger.DebugContext(ctx, "checking for existing component version in target repository", "component", component.Name, "version", component.Version)
		if existing, err := repo.GetComponentVersion(ctx, component.Name, component.Version); err == nil {
			if c.opts.ComponentVersionConflictPolicy == ComponentVersionConflictAbortAndFail {
				return nil, fmt.Errorf("component version %q already exists in target repository", component.ToIdentity())
			}
			logger.WarnContext(ctx, "component version already exists in target repository, skipping construction", "component", component.Name, "version", component.Version)
			return existing, nil
		}
	case ComponentVersionConflictReplace:
		logger.WarnContext(ctx, "REPLACING component version in target repository, old component version will no longer be available")
	}

	if err := c.processDescriptor(ctx, repo, component, desc); err != nil {
		return nil, err
	}

	if err := repo.AddComponentVersion(ctx, desc); err != nil {
		return nil, fmt.Errorf("error adding component version to target: %w", err)
	}

	return desc, nil
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
) error {
	logger := log.Base().With("component", component.Name, "version", component.Version)
	logger.Debug("processing descriptor",
		"num_resources", len(component.Resources),
		"num_sources", len(component.Sources))

	eg, egctx := newConcurrencyGroup(ctx, c.opts.ConcurrencyLimit)
	var descLock sync.Mutex

	for i, resource := range component.Resources {
		resourceLogger := logger.With("resource", resource.ToIdentity())
		resourceLogger.Info("processing resource")

		eg.Go(func() error {
			res, err := c.processResource(egctx, targetRepo, &resource, component.Name, component.Version)
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
		sourceLogger.Info("processing source")

		eg.Go(func() error {
			src, err := c.processSource(egctx, targetRepo, &source, component.Name, component.Version)
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
		logger.Debug("processing resource with input method")
		res, err = c.processResourceWithInput(ctx, targetRepo, resource, component, version)
	case resource.HasAccess():
		if byValue := c.opts.ProcessResourceByValue != nil && c.opts.ProcessResourceByValue(resource); byValue {
			logger.Debug("processing resource by value")
			res, err = c.processResourceByValue(ctx, targetRepo, resource, component, version)
		} else {
			logger.Debug("processing resource with existing access")
			res = constructor.ConvertToDescriptorResource(resource)

			if c.opts.ResourceDigestProcessorProvider != nil {
				var digestProcessor ResourceDigestProcessor
				if digestProcessor, err = c.opts.GetDigestProcessor(ctx, res); err == nil {
					logger.Debug("processing resource digest")
					if res, err = digestProcessor.ProcessResourceDigest(ctx, res); err != nil {
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
	repository, err := c.opts.GetResourceRepository(ctx, resource)
	if err != nil {
		return nil, err
	}

	var creds map[string]string
	if c.opts.CredentialProvider != nil {
		identity, err := repository.GetCredentialConsumerIdentity(ctx, resource)
		if err != nil {
			return nil, fmt.Errorf("error getting credential consumer identity of access type %q: %w", resource.Access.GetType(), err)
		}

		if creds, err = c.opts.Resolve(ctx, identity); err != nil {
			return nil, fmt.Errorf("error resolving credentials for input method of access type %q: %w", resource.Access.GetType(), err)
		}
	}

	converted := constructor.ConvertToDescriptorResource(resource)

	data, err := repository.DownloadResource(ctx, converted, creds)
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

	var creds map[string]string
	if c.opts.CredentialProvider != nil {
		identity, err := method.GetCredentialConsumerIdentity(ctx, src)
		if err != nil {
			return nil, fmt.Errorf("error getting credential consumer identity of type %q: %w", src.Input.GetType(), err)
		}

		if creds, err = c.opts.Resolve(ctx, identity); err != nil {
			return nil, fmt.Errorf("error resolving credentials for input method of type %q: %w", src.Input.GetType(), err)
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

	var creds map[string]string
	if c.opts.CredentialProvider != nil {
		identity, err := method.GetCredentialConsumerIdentity(ctx, resource)
		if err != nil {
			return nil, fmt.Errorf("error getting credential consumer identity of type %q: %w", resource.Input.GetType(), err)
		}

		if creds, err = c.opts.Resolve(ctx, identity); err != nil {
			return nil, fmt.Errorf("error resolving credentials for input method of type %q: %w", resource.Input.GetType(), err)
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

// Validate performs validation checks on a component specification.
// It validates all resources and sources in the component, collecting any validation errors
// and returning them as a single joined error.
func Validate(component *constructor.Component) error {
	return component.Validate()
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
	localBlob := &descriptor.LocalBlob{}
	localBlob.SetType(descriptor.GetLocalBlobAccessType())

	if mediaTypeAware, ok := data.(blob.MediaTypeAware); ok {
		localBlob.MediaType, _ = mediaTypeAware.MediaType()
	}
	if localBlob.MediaType == "" {
		localBlob.MediaType = "application/octet-stream"
	}

	// if the resource doesn't have any information about its relation to the component
	// default to a local resource.
	if resource.Relation == "" {
		resource.Relation = constructor.LocalRelation
	}

	// if the resource doesn't have any information about its version,
	// default to the component version.
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
