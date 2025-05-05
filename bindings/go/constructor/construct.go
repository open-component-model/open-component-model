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

type Options struct {
	Target              input.ComponentVersionRepository
	InputMethodRegistry *registry.Registry
}

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

func construct(ctx context.Context, component spec.Component, opts Options) (*descriptor.Descriptor, error) {
	if err := Validate(component); err != nil {
		return nil, err
	}

	desc := descriptor.Descriptor{
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
	var descLock sync.Mutex

	eg, egctx := errgroup.WithContext(ctx)
	eg.SetLimit(goruntime.NumCPU())
	for i, resource := range component.Resources {
		eg.Go(func() error {
			var res *descriptor.Resource
			if resource.HasInput() {
				method, found := opts.InputMethodRegistry.GetFor(resource.Input)
				if !found {
					return fmt.Errorf("no input method found for input specification of type %q", resource.Input.GetType())
				}
				var err error
				res, err = method.ProcessResource(egctx, &resource, input.Options{
					Component: component.Name,
					Version:   component.Version,
					Target:    opts.Target,
				})
				if err != nil {
					return fmt.Errorf("error getting blob from input method: %w", err)
				}
				desc.Component.Resources[i] = *res
			} else {
				desc.Component.Resources[i] = spec.ConvertToRuntimeResource(resource)
			}

			if desc.Component.Resources[i].Access == nil {
				return fmt.Errorf("after the input method was processed, no access was present in the resource. This is likely a problem in the input method")
			}

			descLock.Lock()
			defer descLock.Unlock()

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("error constructing component: %w", err)
	}

	if err := opts.Target.AddComponentVersion(ctx, &desc); err != nil {
		return nil, fmt.Errorf("error adding component version to target: %w", err)
	}

	return &desc, nil
}

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
