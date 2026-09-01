package constructor

import (
	"errors"
	"fmt"

	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
)

// validateSpecifications checks the access and input specification of every resource and source of
// the given constructor with constructor.AccessOrInput.Validate.
//
// All violations are reported together, so that a single run reports every faulty specification.
func validateSpecifications(componentConstructor *constructor.ComponentConstructor, opts Options) error {
	if opts.AccessSpecificationScheme == nil && opts.InputSpecificationScheme == nil {
		return nil
	}

	var errs []error
	for i := range componentConstructor.Components {
		component := &componentConstructor.Components[i]
		for j := range component.Resources {
			resource := &component.Resources[j]
			if err := resource.AccessOrInput.ValidateWithSchemes(opts.AccessSpecificationScheme, opts.InputSpecificationScheme); err != nil {
				errs = append(errs, fmt.Errorf("resource %q of component %q: %w", resource.ToIdentity(), component.ToIdentity(), err))
			}
		}
		for j := range component.Sources {
			source := &component.Sources[j]
			if err := source.AccessOrInput.ValidateWithSchemes(opts.AccessSpecificationScheme, opts.InputSpecificationScheme); err != nil {
				errs = append(errs, fmt.Errorf("source %q of component %q: %w", source.ToIdentity(), component.ToIdentity(), err))
			}
		}
	}

	return errors.Join(errs...)
}
