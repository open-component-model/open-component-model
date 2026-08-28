package constructor

import (
	"errors"
	"fmt"

	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// validateSpecifications checks the access and input specification of every resource and source of
// the given constructor.
//
// A specification is decoded into the type registered for it and, if that type implements
// ocmruntime.Validatable, checked with Validate. Specifications of a type that is not registered are
// not validated, so that component versions can use access and input types unknown to the
// constructor.
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
			if err := validateAccessOrInput(&resource.AccessOrInput, opts); err != nil {
				errs = append(errs, fmt.Errorf("resource %q of component %q: %w", resource.ToIdentity(), component.ToIdentity(), err))
			}
		}
		for j := range component.Sources {
			source := &component.Sources[j]
			if err := validateAccessOrInput(&source.AccessOrInput, opts); err != nil {
				errs = append(errs, fmt.Errorf("source %q of component %q: %w", source.ToIdentity(), component.ToIdentity(), err))
			}
		}
	}

	return errors.Join(errs...)
}

func validateAccessOrInput(accessOrInput *constructor.AccessOrInput, opts Options) error {
	switch {
	case accessOrInput.HasAccess():
		if err := validateSpecification(accessOrInput.Access, opts.AccessSpecificationScheme); err != nil {
			return fmt.Errorf("access specification: %w", err)
		}
	case accessOrInput.HasInput():
		if err := validateSpecification(accessOrInput.Input, opts.InputSpecificationScheme); err != nil {
			return fmt.Errorf("input specification: %w", err)
		}
	}

	return nil
}

// validateSpecification decodes spec into the type registered for it and, if that type implements
// ocmruntime.Validatable, checks it with Validate. A specification of a type that is not registered
// is not validated.
func validateSpecification(spec ocmruntime.Typed, scheme *ocmruntime.Scheme) error {
	if scheme == nil {
		return nil
	}
	typ := spec.GetType()
	if typ.IsEmpty() || !scheme.IsRegistered(typ) {
		return nil
	}

	obj, err := scheme.NewObject(typ)
	if err != nil {
		return fmt.Errorf("cannot create an object of type %q: %w", typ, err)
	}
	if err := scheme.Convert(spec, obj); err != nil {
		return fmt.Errorf("type %q cannot be decoded: %w", typ, err)
	}

	validatable, ok := obj.(ocmruntime.Validatable)
	if !ok {
		return nil
	}
	if err := validatable.Validate(); err != nil {
		return fmt.Errorf("type %q is invalid: %w", typ, err)
	}

	return nil
}
