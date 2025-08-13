package constructor

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor/internal/log"
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// ErrShouldSkipConstruction is an error that indicates that the construction of a component should be skipped,
// e.g. because the component version already exists in the target repository.
var ErrShouldSkipConstruction = errors.New("should skip construction")

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
		componentLogger.Debug("constructing component")

		eg.Go(func() error {
			if c.opts.OnStartComponentConstruct != nil {
				if err := c.opts.OnStartComponentConstruct(egctx, &component); err != nil {
					return fmt.Errorf("error starting component construction for %q: %w", component.ToIdentity(), err)
				}
			}
			desc, err := c.construct(egctx, &component)
			if c.opts.OnEndComponentConstruct != nil {
				if err := c.opts.OnEndComponentConstruct(egctx, desc, err); err != nil {
					return fmt.Errorf("error ending component construction for %q: %w", component.ToIdentity(), err)
				}
			}
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

	logger.Debug("component construction completed successfully", "num_components", len(descriptors))
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

	if err := c.processDescriptor(ctx, repo, component, desc); err != nil {
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
					if c.opts.CredentialProvider != nil {
						identity, err := digestProcessor.GetResourceDigestProcessorCredentialConsumerIdentity(ctx, res)
						if err != nil {
							return nil, fmt.Errorf("error getting credential consumer identity of access type %q: %w", resource.Access.GetType(), err)
						}

						if creds, err = c.opts.Resolve(ctx, identity); err != nil {
							return nil, fmt.Errorf("error resolving credentials for input method of access type %q: %w", resource.Access.GetType(), err)
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
		if creds, err = resolveCredentials(ctx, c.opts.CredentialProvider, identity); err != nil {
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

	overrideWorkingDirectory(method, c.opts.SpecFileDir())

	// best effort to resolve credentials for the input method.
	// if no identity is resolved, we assume resolution is simply skipped.
	var creds map[string]string
	if identity, err := method.GetSourceCredentialConsumerIdentity(ctx, src); err == nil {
		if creds, err = resolveCredentials(ctx, c.opts.CredentialProvider, identity); err != nil {
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

	overrideWorkingDirectory(method, c.opts.SpecFileDir())

	// best effort to resolve credentials for the input method.
	// if no identity is resolved, we assume resolution is simply skipped.
	var creds map[string]string
	if identity, err := method.GetResourceCredentialConsumerIdentity(ctx, resource); err == nil {
		if creds, err = resolveCredentials(ctx, c.opts.CredentialProvider, identity); err != nil {
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
func resolveCredentials(ctx context.Context, provider CredentialProvider, consumerIdentity ocmruntime.Identity) (map[string]string, error) {
	logger := log.Base().With("identity", consumerIdentity)

	if provider == nil {
		logger.DebugContext(ctx, "no credential provider configured, skipping credential resolution")
		return nil, nil
	}

	if consumerIdentity == nil {
		logger.DebugContext(ctx, "no credential consumer identity found, proceeding without credentials")
		return nil, nil
	}

	return provider.Resolve(ctx, consumerIdentity)
}

// overrideWorkingDirectory overrides the working directory for a given WorkingDirectoryOverride.
// This is useful for methods that require a specific working directory to operate correctly.
// If the method does not implement WorkingDirectoryOverride, it will be ignored.
// If the working directory is an absolute path, it will not be overridden.
func overrideWorkingDirectory(method any, workingDir string) {
	if method == nil {
		return
	}

	if wdOverride, ok := method.(WorkingDirectoryOverride); ok && wdOverride.GetWd() == "" {
		// only override the working directory if it is empty
		// it might have been set by the input method already
		// causes could be the working-directory flag in the cli
		wdOverride.SetWd(workingDir)
	}
}
