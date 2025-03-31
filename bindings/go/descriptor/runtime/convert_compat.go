package runtime

import (
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// CompatibilityConversionOptions defines the configuration for legacy type conversion.
// KnownLegacyTypes maps old type identifiers to their new equivalents.
type CompatibilityConversionOptions struct {
	KnownLegacyTypes map[runtime.Type]runtime.Type
}

// compatibilityConvert handles the conversion of a single typed object.
// It checks if the type is registered in the scheme, and if not, looks up its legacy type mapping.
// If a mapping exists, it converts the type to its new equivalent.
// If no mapping exists, it returns an error.
func compatibilityConvert(scheme *runtime.Scheme, access runtime.Typed, opts *CompatibilityConversionOptions) (runtime.Typed, error) {
	if access == nil {
		return nil, nil
	}

	typ := access.GetType()
	if !scheme.IsRegistered(typ) {
		newtyp, ok := opts.KnownLegacyTypes[typ]
		if !ok {
			return nil, fmt.Errorf("type %q is not registered and no known legacy type found", typ)
		}
		access.SetType(newtyp)
		typ = newtyp
	}

	newTyped, err := scheme.NewObject(typ)
	if err != nil {
		return nil, fmt.Errorf("new access type could not be created: %w", err)
	}

	if err := scheme.Convert(access, newTyped); err != nil {
		return nil, fmt.Errorf("access type could not be converted: %w", err)
	}
	newTyped.SetType(typ)

	return newTyped, nil
}

// CompatibilityConvert handles the conversion of legacy access types in component descriptors to their new equivalents.
// It processes both resource and source access types in the descriptor.
//
// The conversion process works as follows:
// 1. For each resource and source in the descriptor:
//   - If the access type is not registered in the scheme, look up its legacy type mapping
//   - If a mapping exists, convert the type to its new equivalent
//   - If no mapping exists, collect an error
//
// 2. Return all collected errors as a combined error
//
// Parameters:
//   - scheme: The runtime scheme that defines the set of known types
//   - descriptor: The component descriptor containing resources and sources to convert
//   - opts: Conversion options containing the mapping of legacy types to new types
//
// Returns:
//   - An error if any type conversion fails, combining all conversion errors
func CompatibilityConvert(scheme *runtime.Scheme, descriptor *Descriptor, opts *CompatibilityConversionOptions) error {
	var errs []error

	// Convert resource access types
	for i, resource := range descriptor.Component.Resources {
		newAccess, err := compatibilityConvert(scheme, resource.Access, opts)
		if err != nil {
			errs = append(errs, fmt.Errorf("resource access type at resource index %d could not be converted: %w", i, err))
			continue
		}
		resource.Access = newAccess
		descriptor.Component.Resources[i] = resource
	}

	// Convert source access types
	for i, source := range descriptor.Component.Sources {
		newAccess, err := compatibilityConvert(scheme, source.Access, opts)
		if err != nil {
			errs = append(errs, fmt.Errorf("source access type at source index %d could not be converted: %w", i, err))
			continue
		}
		source.Access = newAccess
		descriptor.Component.Sources[i] = source
	}

	return errors.Join(errs...)
}
