package constructor

import (
	"context"
	"errors"
	"fmt"
	goruntime "runtime"
	"sync"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/constructor/input"
	"ocm.software/open-component-model/bindings/go/constructor/input/registry"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Repository defines the interface for a target repository that can store component versions but also
// act as a target for the constructor to add resources to.
type Repository interface {
	input.TargetRepository
	// AddComponentVersion adds a new component version to the repository.
	// If a component version already exists, it will be updated with the new descriptor.
	// The descriptor internally will be serialized via the runtime package.
	// The descriptor MUST have its target Name and Version already set as they are used to identify the target
	// Location in the Store.
	AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error
}

type Options struct {
	Target              Repository
	InputMethodRegistry *registry.Registry
}

// Construct processes a component constructor specification and creates the corresponding component descriptors.
// It validates the constructor specification and processes each component in sequence.
func Construct(ctx context.Context, constructor *spec.ComponentConstructor, opts Options) ([]*descriptor.Descriptor, error) {
	if err := spec.Validate(constructor); err != nil {
		return nil, err
	}
	if opts.InputMethodRegistry == nil {
		opts.InputMethodRegistry = registry.Default
	}
	descriptors := make([]*descriptor.Descriptor, 0)
	for _, component := range constructor.Components {
		desc, err := construct(ctx, component, opts)
		if err != nil {
			return nil, err
		}
		descriptors = append(descriptors, desc)
	}
	return descriptors, nil
}

// construct creates a single component descriptor from a component specification.
// It handles the creation of the base descriptor, processes all resources concurrently,
// and adds the final component version to the target repository.
func construct(ctx context.Context, component spec.Component, opts Options) (*descriptor.Descriptor, error) {
	if err := Validate(component); err != nil {
		return nil, err
	}

	desc := createBaseDescriptor(component)
	var descLock sync.Mutex

	if err := processResources(ctx, component, opts, desc, &descLock); err != nil {
		return nil, err
	}

	if err := opts.Target.AddComponentVersion(ctx, desc); err != nil {
		return nil, fmt.Errorf("error adding component version to target: %w", err)
	}

	return desc, nil
}

// createBaseDescriptor initializes a new descriptor with the basic component metadata.
// It sets up the component name, version, labels, and provider information, and prepares
// empty slices for resources, sources, references, and repository contexts.
func createBaseDescriptor(component spec.Component) *descriptor.Descriptor {
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v2",
		},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    component.Name,
					Version: component.Version,
					Labels:  spec.ConvertFromLabels(component.Labels),
				},
			},
			Provider: map[string]string{
				"name": component.Provider.Name,
			},
			Resources:          make([]descriptor.Resource, len(component.Resources)),
			Sources:            make([]descriptor.Source, 0),
			References:         make([]descriptor.Reference, 0),
			RepositoryContexts: make([]runtime.Typed, 0),
		},
	}
}

// processResources handles the concurrent processing of all resources in a component.
// It uses an errgroup to manage concurrent resource processing with a limit based on
// the number of available CPU cores.
func processResources(ctx context.Context, component spec.Component, opts Options, desc *descriptor.Descriptor, descLock *sync.Mutex) error {
	eg, egctx := errgroup.WithContext(ctx)
	eg.SetLimit(goruntime.NumCPU())

	for i, resource := range component.Resources {
		eg.Go(func() error {
			return processResource(egctx, i, resource, component, opts, desc, descLock)
		})
	}

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("error constructing component: %w", err)
	}

	return nil
}

// processResource handles the processing of a single resource, including both input and non-input cases.
// It ensures thread-safe access to the descriptor when updating resource information
// and validates that the processed resource has proper access information.
func processResource(ctx context.Context, index int, resource spec.Resource, component spec.Component, opts Options, desc *descriptor.Descriptor, descLock *sync.Mutex) error {
	var res *descriptor.Resource
	var err error

	if resource.HasInput() {
		res, err = processResourceWithInput(ctx, resource, component, opts)
		if err != nil {
			return err
		}
		descLock.Lock()
		defer descLock.Unlock()
		desc.Component.Resources[index] = *res
	} else {
		descLock.Lock()
		defer descLock.Unlock()
		desc.Component.Resources[index] = spec.ConvertToRuntimeResource(resource)
	}

	if desc.Component.Resources[index].Access == nil {
		return fmt.Errorf("after the input method was processed, no access was present in the resource. This is likely a problem in the input method")
	}

	return nil
}

// processResourceWithInput handles the specific case of processing a resource that has an input method.
// It looks up the appropriate input method from the registry and processes the resource
// using the found method.
func processResourceWithInput(ctx context.Context, resource spec.Resource, component spec.Component, opts Options) (*descriptor.Resource, error) {
	method, found := opts.InputMethodRegistry.GetFor(resource.Input)
	if !found {
		return nil, fmt.Errorf("no input method found for input specification of type %q", resource.Input.GetType())
	}

	res, err := method.ProcessResource(ctx, &resource, input.Options{
		Component: component.Name,
		Version:   component.Version,
		Target:    opts.Target,
	})
	if err != nil {
		return nil, fmt.Errorf("error getting blob from input method: %w", err)
	}

	return res, nil
}

// Validate performs validation checks on a component specification.
// It validates all resources and sources in the component, collecting any validation errors
// and returning them as a single joined error.
func Validate(component spec.Component) error {
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
