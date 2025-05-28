package constructor

import (
	"context"
	"fmt"

	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	_ ResourceInputMethodProvider = (*InputMethodRegistry)(nil)
	_ SourceInputMethodProvider   = (*InputMethodRegistry)(nil)
)

// InputMethodRegistry manages resource input resourceMethods for different types
type InputMethodRegistry struct {
	resourceMethods map[runtime.Type]ResourceInputMethod
	sourceMethods   map[runtime.Type]SourceInputMethod
	scheme          *runtime.Scheme
}

// New creates a new InputMethodRegistry instance with the given scheme
func New(scheme *runtime.Scheme) *InputMethodRegistry {
	return &InputMethodRegistry{
		scheme:          scheme,
		resourceMethods: make(map[runtime.Type]ResourceInputMethod),
		sourceMethods:   make(map[runtime.Type]SourceInputMethod),
	}
}

// RegisterResourceInputMethod registers a resource input method for a given prototype type.
// Returns an error if the registration fails or if the type is already registered.
func (r *InputMethodRegistry) RegisterResourceInputMethod(prototype runtime.Typed, method ResourceInputMethod) error {
	typ, err := r.scheme.TypeForPrototype(prototype)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype: %w", err)
	}
	if _, exists := r.resourceMethods[typ]; exists {
		return fmt.Errorf("resource input method for type %q is already registered", typ)
	}
	r.resourceMethods[typ] = method
	return nil
}

// MustRegisterResourceInputMethod registers a resource input method for a given prototype type.
// Panics if the registration fails or if the type is already registered.
func (r *InputMethodRegistry) MustRegisterResourceInputMethod(prototype runtime.Typed, method ResourceInputMethod) {
	if err := r.RegisterResourceInputMethod(prototype, method); err != nil {
		panic(err)
	}
}

func (r *InputMethodRegistry) typeInsideRegistry(input runtime.Typed) (runtime.Type, error) {
	inputType := input.GetType()

	typed, err := r.scheme.NewObject(inputType)
	if err != nil {
		return runtime.Type{}, err
	}

	typ, err := r.scheme.TypeForPrototype(typed)
	if err != nil {
		return runtime.Type{}, err
	}

	return typ, nil
}

// GetResourceInputMethod retrieves the resource input method for a given typed object.
func (r *InputMethodRegistry) GetResourceInputMethod(_ context.Context, res *constructorv1.Resource) (ResourceInputMethod, error) {
	if res == nil || !res.HasInput() {
		return nil, fmt.Errorf("resource input method requested for resource without input: %v", res)
	}

	typ, err := r.typeInsideRegistry(res.Input)
	if err != nil {
		return nil, fmt.Errorf("error getting resource input method: %w", err)
	}
	method, ok := r.resourceMethods[typ]
	if !ok {
		return nil, fmt.Errorf("no input method found for type %q", typ)
	}

	return method, nil
}

// GetSourceInputMethod retrieves the source input method for a given typed object.
func (r *InputMethodRegistry) GetSourceInputMethod(_ context.Context, src *constructorv1.Source) (SourceInputMethod, error) {
	if src == nil || !src.HasInput() {
		return nil, fmt.Errorf("source input method requested for source without input: %v", src)
	}

	typ, err := r.typeInsideRegistry(src.Input)
	if err != nil {
		return nil, fmt.Errorf("error getting source input method: %w", err)
	}
	method, ok := r.sourceMethods[typ]
	if !ok {
		return nil, fmt.Errorf("no input method found for type %q", typ)
	}

	return method, nil
}

// RegisterSourceInputMethod registers a source input method for a given prototype type.
// Returns an error if the registration fails or if the type is already registered.
func (r *InputMethodRegistry) RegisterSourceInputMethod(prototype runtime.Typed, method SourceInputMethod) error {
	typ, err := r.scheme.TypeForPrototype(prototype)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype: %w", err)
	}
	if _, exists := r.sourceMethods[typ]; exists {
		return fmt.Errorf("source input method for type %q is already registered", typ)
	}
	r.sourceMethods[typ] = method
	return nil
}

// MustRegisterSourceInputMethod registers a source input method for a given prototype type.
// Panics if the registration fails or if the type is already registered.
func (r *InputMethodRegistry) MustRegisterSourceInputMethod(prototype runtime.Typed, method SourceInputMethod) {
	if err := r.RegisterSourceInputMethod(prototype, method); err != nil {
		panic(err)
	}
}
