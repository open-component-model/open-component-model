package runtime

import (
	"fmt"
	"io"
	"maps"
	"reflect"
	"sync"

	"sigs.k8s.io/yaml"
)

// Scheme is a dynamic registry for Typed types.
type Scheme struct {
	mu sync.RWMutex
	// allowUnknown allows unknown types to be created.
	// if the constructors cannot determine a match,
	// this will trigger the creation of an unstructured.Unstructured with NewScheme instead of failing.
	allowUnknown bool
	types        map[Type]Typed
}

// NewScheme creates a new registry.
func NewScheme(opts ...SchemeOption) *Scheme {
	reg := &Scheme{
		types: make(map[Type]Typed),
	}
	for _, opt := range opts {
		opt(reg)
	}
	return reg
}

type SchemeOption func(*Scheme)

// WithAllowUnknown allows unknown types to be created.
func WithAllowUnknown(allowUnknown bool) SchemeOption {
	return func(registry *Scheme) {
		registry.allowUnknown = allowUnknown
	}
}

func (r *Scheme) Clone() *Scheme {
	r.mu.RLock()
	defer r.mu.RUnlock()
	clone := NewScheme(WithAllowUnknown(r.allowUnknown))
	maps.Copy(r.types, clone.types)
	return clone
}

func (r *Scheme) RegisterWithAlias(prototype Typed, types ...Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, typ := range types {
		if _, exists := r.types[typ.GetType()]; exists {
			return fmt.Errorf("type %q is already registered", typ)
		}
		r.types[typ.GetType()] = prototype
	}
	return nil
}

func (r *Scheme) MustRegister(prototype Typed, version string) {
	t := reflect.TypeOf(prototype)
	if t.Kind() != reflect.Pointer {
		panic("All types must be pointers to structs.")
	}
	t = t.Elem()
	r.MustRegisterWithAlias(prototype, NewType(t.Name(), version))
}

func (r *Scheme) TypeForPrototype(prototype Typed) (Type, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for typ, proto := range r.types {
		// if there is an unversioned type registered, do not use it
		// TODO find a way to avoid this
		if typ.GetVersion() == "" {
			continue
		}
		if reflect.TypeOf(prototype).Elem() == reflect.TypeOf(proto).Elem() {
			return typ, nil
		}
	}

	return "", fmt.Errorf("prototype not found in registry")
}

func (r *Scheme) MustTypeForPrototype(prototype Typed) Type {
	typ, err := r.TypeForPrototype(prototype)
	if err != nil {
		panic(err)
	}
	return typ
}

func (r *Scheme) IsRegistered(typ Type) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.types[typ]
	return exists
}

func (r *Scheme) MustRegisterWithAlias(prototype Typed, types ...Type) {
	if err := r.RegisterWithAlias(prototype, types...); err != nil {
		panic(err)
	}
}

// NewObject creates a new instance of types.Typed.
func (r *Scheme) NewObject(typ Type) (Typed, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var object any
	// construct by full type
	proto, exists := r.types[typ]
	if exists {
		t := reflect.TypeOf(proto)
		for t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		object = reflect.New(t).Interface()

		return object.(Typed), nil
	}

	if r.allowUnknown {
		return &Raw{}, nil
	}

	return nil, fmt.Errorf("unsupported type: %s", typ)
}

func (r *Scheme) Decode(data io.Reader, into Typed) error {
	if _, err := r.TypeForPrototype(into); err != nil && !r.allowUnknown {
		return fmt.Errorf("%T is not a valid registered type and cannot be decoded: %w", into, err)
	}
	bytes, err := io.ReadAll(data)
	if err != nil {
		return fmt.Errorf("could not read data: %w", err)
	}
	if err := yaml.Unmarshal(bytes, into); err != nil {
		return fmt.Errorf("failed to unmarshal raw: %w", err)
	}
	return nil
}

func (r *Scheme) Convert(from Typed, into Typed) error {
	// check if typed is a raw, yaml unmarshalling has its own reflection check so we don't need to do this
	// before the raw assertion.
	if raw, ok := from.(*Raw); ok {
		if _, err := r.TypeForPrototype(into); err != nil && !r.allowUnknown {
			return fmt.Errorf("%T is not a valid registered type and cannot be decoded: %w", into, err)
		}
		if !r.IsRegistered(from.GetType()) {
			return fmt.Errorf("cannot decode from unregistered type: %s", from.GetType())
		}
		if err := yaml.Unmarshal(raw.Data, into); err != nil {
			return fmt.Errorf("failed to unmarshal raw: %w", err)
		}
		return nil
	}

	intoValue := reflect.ValueOf(into)
	if intoValue.Kind() != reflect.Ptr || intoValue.IsNil() {
		return fmt.Errorf("into must be a non-nil pointer")
	}

	fromValue := reflect.ValueOf(from)
	if fromValue.Kind() == reflect.Ptr {
		fromValue = fromValue.Elem()
	}

	if !fromValue.IsValid() || fromValue.IsZero() {
		return fmt.Errorf("from must be a non-nil pointer")
	}

	if fromValue.Type() != intoValue.Elem().Type() {
		return fmt.Errorf("from and into must be the same type, cannot decode from %s into %s", fromValue.Type(), intoValue.Elem().Type())
	}

	// set the pointer value of into to the new object pointer
	intoValue.Elem().Set(fromValue)
	return nil
}
