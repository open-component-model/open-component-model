package constructor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	goruntime "runtime"
	"sync"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type Constructor interface {
	// Construct processes a component constructor specification and creates the corresponding component descriptors.
	// It validates the constructor specification and processes each component in sequence.
	Construct(ctx context.Context, constructor *constructorv1.ComponentConstructor) ([]*descriptor.Descriptor, error)
}

// ConstructDefault is a convenience function that creates a new default DefaultConstructor and calls its Constructor.Construct method.
func ConstructDefault(ctx context.Context, constructor *constructorv1.ComponentConstructor, opts Options) ([]*descriptor.Descriptor, error) {
	return NewDefaultConstructor(opts).Construct(ctx, constructor)
}

type DefaultConstructor struct {
	opts Options
}

func (c *DefaultConstructor) Construct(ctx context.Context, constructor *constructorv1.ComponentConstructor) ([]*descriptor.Descriptor, error) {
	logger := slog.With("operation", "construct")

	if err := constructorv1.Validate(constructor); err != nil {
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
		componentLogger.Debug("processing component")

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
func (c *DefaultConstructor) construct(ctx context.Context, component *constructorv1.Component) (*descriptor.Descriptor, error) {
	logger := slog.With("component", component.Name, "version", component.Version)
	logger.Debug("starting component construction")

	if err := Validate(component); err != nil {
		return nil, err
	}

	desc := createBaseDescriptor(component)
	logger.Debug("created base descriptor")

	repo, err := c.opts.TargetRepositoryProvider.GetTargetRepository(ctx, component)
	if err != nil {
		return nil, fmt.Errorf("error getting target repository for component %q: %w", component.Name, err)
	}

	if err := c.processDescriptor(ctx, repo, component, desc); err != nil {
		return nil, err
	}

	if err := repo.AddComponentVersion(ctx, desc); err != nil {
		return nil, fmt.Errorf("error adding component version to target: %w", err)
	}

	logger.Debug("component construction completed successfully")
	return desc, nil
}

// createBaseDescriptor initializes a new descriptor with the basic component metadata.
// It sets up the component name, version, labels, and provider information, and prepares
// empty slices for resources, sources, references, and repository contexts.
func createBaseDescriptor(component *constructorv1.Component) *descriptor.Descriptor {
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v2",
		},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    component.Name,
					Version: component.Version,
					Labels:  constructorv1.ConvertFromLabels(component.Labels),
				},
			},
			Provider: map[string]string{
				"name": component.Provider.Name,
			},
			Resources:          make([]descriptor.Resource, len(component.Resources)),
			Sources:            make([]descriptor.Source, len(component.Sources)),
			References:         make([]descriptor.Reference, 0),
			RepositoryContexts: make([]runtime.Typed, 0),
		},
	}
}

// processDescriptor handles the concurrent processing of all resources and sources in a component.
// It uses an errgroup to manage concurrent resource processing with a limit based on
// the number of available CPU cores.
func (c *DefaultConstructor) processDescriptor(
	ctx context.Context,
	targetRepo TargetRepository,
	component *constructorv1.Component,
	desc *descriptor.Descriptor,
) error {
	logger := slog.With("component", component.Name, "version", component.Version)
	logger.Debug("processing descriptor",
		"num_resources", len(component.Resources),
		"num_sources", len(component.Sources))

	eg, egctx := newConcurrencyGroup(ctx, c.opts.ConcurrencyLimit)
	var descLock sync.Mutex

	for i, resource := range component.Resources {
		resourceLogger := logger.With("resource", resource.ToIdentity())
		resourceLogger.Debug("processing resource")

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
		sourceLogger.Debug("processing source")

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
func (c *DefaultConstructor) processResource(ctx context.Context, targetRepo TargetRepository, resource *constructorv1.Resource, component, version string) (*descriptor.Resource, error) {
	logger := slog.With(
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
			converted := constructorv1.ConvertToRuntimeResource(*resource)
			res = &converted

			if c.opts.ResourceDigestProcessorProvider != nil {
				var digestProcessor ResourceDigestProcessor
				if digestProcessor, err = c.opts.ResourceDigestProcessorProvider.GetDigestProcessor(ctx, res); err == nil {
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

func (c *DefaultConstructor) processResourceByValue(ctx context.Context, targetRepo TargetRepository, resource *constructorv1.Resource, component, version string) (*descriptor.Resource, error) {
	repository, err := c.opts.ResourceRepositoryProvider.GetResourceRepository(ctx, resource)
	if err != nil {
		return nil, err
	}
	converted := constructorv1.ConvertToRuntimeResource(*resource)
	data, err := repository.DownloadResource(ctx, &converted)
	if err != nil {
		return nil, fmt.Errorf("error downloading resource: %w", err)
	}
	return addColocatedResourceLocalBlob(ctx, targetRepo, component, version, resource, data)
}

func (c *DefaultConstructor) processSource(ctx context.Context, targetRepo TargetRepository, src *constructorv1.Source, component, version string) (*descriptor.Source, error) {
	logger := slog.With(
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
		converted := constructorv1.ConvertToRuntimeSource(*src)
		res = &converted
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
func (c *DefaultConstructor) processSourceWithInput(ctx context.Context, targetRepo TargetRepository, src *constructorv1.Source, component, version string) (*descriptor.Source, error) {
	method, err := c.opts.SourceInputMethodProvider.GetSourceInputMethod(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("no input method resolvable for input specification of type %q: %w", src.Input.GetType(), err)
	}

	var creds map[string]string
	if c.opts.CredentialProvider != nil {
		identity, err := method.GetCredentialConsumerIdentity(ctx, src)
		if err != nil {
			return nil, fmt.Errorf("error getting credential consumer identity of type %q: %w", src.Input.GetType(), err)
		}

		if creds, err = c.opts.CredentialProvider.Resolve(ctx, identity); err != nil {
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
func (c *DefaultConstructor) processResourceWithInput(ctx context.Context, targetRepo TargetRepository, resource *constructorv1.Resource, component, version string) (*descriptor.Resource, error) {
	method, err := c.opts.ResourceInputMethodProvider.GetResourceInputMethod(ctx, resource)
	if err != nil {
		return nil, fmt.Errorf("no input method resolvable for input specification of type %q: %w", resource.Input.GetType(), err)
	}

	var creds map[string]string
	if c.opts.CredentialProvider != nil {
		identity, err := method.GetCredentialConsumerIdentity(ctx, resource)
		if err != nil {
			return nil, fmt.Errorf("error getting credential consumer identity of type %q: %w", resource.Input.GetType(), err)
		}

		if creds, err = c.opts.CredentialProvider.Resolve(ctx, identity); err != nil {
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
func Validate(component *constructorv1.Component) error {
	errs := make([]error, 0, len(component.Resources)+len(component.Sources))
	for _, resource := range component.Resources {
		if err := resource.Validate(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, source := range component.Sources {
		if err := source.Validate(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// addColocatedResourceLocalBlob adds a local blob to the component version repository and defaults fields relevant
// to declare the spec.LocalRelation to the component version as well as default the resource version, media type and size.
// The resource is expected to be a local resource so the access that is created is always a local blob.
func addColocatedResourceLocalBlob(
	ctx context.Context,
	repo TargetRepository,
	component, version string,
	resource *constructorv1.Resource,
	data blob.ReadOnlyBlob,
) (processed *descriptor.Resource, err error) {
	localBlob := &v2.LocalBlob{}
	if _, err := v2.Scheme.DefaultType(localBlob); err != nil {
		return nil, fmt.Errorf("error getting default type for local blob: %w", err)
	}

	if mediaTypeAware, ok := data.(blob.MediaTypeAware); ok {
		localBlob.MediaType, _ = mediaTypeAware.MediaType()
	}

	// if the resource doesn't have any information about its relation to the component
	// default to a local resource.
	if resource.Relation == "" {
		resource.Relation = constructorv1.LocalRelation
	}

	// if the resource doesn't have any information about its version,
	// default to the component version.
	if resource.Version == "" {
		resource.Version = version
	}

	descResource := constructorv1.ConvertToRuntimeResource(*resource)

	descResource.Access = localBlob
	uploaded, err := repo.AddLocalResource(ctx, component, version, &descResource, data)
	if err != nil {
		return nil, fmt.Errorf("error adding local resource %q based on input type %q as local resource to component %q : %w", resource.ToIdentity(), resource.Input.GetType(), component, err)
	}

	return uploaded, nil
}

func addColocatedSourceLocalBlob(
	ctx context.Context,
	repo TargetRepository,
	component, version string,
	source *constructorv1.Source,
	data blob.ReadOnlyBlob,
) (processed *descriptor.Source, err error) {
	localBlob := &v2.LocalBlob{}
	if _, err := v2.Scheme.DefaultType(localBlob); err != nil {
		return nil, fmt.Errorf("error getting default type for local blob: %w", err)
	}

	if mediaTypeAware, ok := data.(blob.MediaTypeAware); ok {
		localBlob.MediaType, _ = mediaTypeAware.MediaType()
	}

	// if the source doesn't have any information about its version,
	// default to the component version.
	if source.Version == "" {
		source.Version = version
	}

	descSource := constructorv1.ConvertToRuntimeSource(*source)

	descSource.Access = localBlob
	uploaded, err := repo.AddLocalSource(ctx, component, version, &descSource, data)
	if err != nil {
		return nil, fmt.Errorf("error adding local source %q based on input type %q as local resource to component %q : %w", source.ToIdentity(), source.Input.GetType(), component, err)
	}

	return uploaded, nil
}

func newConcurrencyGroup(ctx context.Context, limit int) (*errgroup.Group, context.Context) {
	logger := slog.With("operation", "new_concurrency_group")

	eg, egctx := errgroup.WithContext(ctx)

	if limit > 0 {
		logger.Debug("setting custom concurrency limit", "limit", limit)
		eg.SetLimit(limit)
	} else {
		cores := goruntime.NumCPU()
		logger.Debug("using CPU core count as concurrency limit", "cores", cores)
		eg.SetLimit(cores)
	}
	return eg, egctx
}
