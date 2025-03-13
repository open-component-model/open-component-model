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
	types        map[Type]any
}

// NewScheme creates a new registry.
func NewScheme(opts ...SchemeOption) *Scheme {
	reg := &Scheme{
		types: make(map[Type]any),
	}
	for _, opt := range opts {
		opt(reg)
	}
	return reg
}

type SchemeOption func(*Scheme)

// WithAllowUnknown allows unknown types to be created.
func WithAllowUnknown() SchemeOption {
	return func(registry *Scheme) {
		registry.allowUnknown = true
	}
}

func (r *Scheme) Clone() *Scheme {
	r.mu.RLock()
	defer r.mu.RUnlock()
	clone := NewScheme()
	clone.allowUnknown = r.allowUnknown
	maps.Copy(clone.types, r.types)
	return clone
}

func (r *Scheme) RegisterWithAlias(prototype any, types ...Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, typ := range types {
		if _, exists := r.types[typ]; exists {
			return fmt.Errorf("type %q is already registered", typ)
		}
		r.types[typ] = prototype
	}
	return nil
}

// GetTypeFromAny uses reflection to extract the "Type" field from any struct.
func GetTypeFromAny(v any) (Type, error) {
	val := reflect.ValueOf(v)

	// Ensure v is a struct or a pointer to a struct
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return Type{}, fmt.Errorf("expected struct, got %s", val.Kind())
	}

	// Get the field by name
	field := val.FieldByName("Type")
	if !field.IsValid() {
		return Type{}, fmt.Errorf("field 'Type' not found")
	}

	// Ensure it's of Type type
	if field.Type() != reflect.TypeOf(Type{}) {
		return Type{}, fmt.Errorf("field 'Type' is not of expected Type struct")
	}

	// Return the Type value
	return field.Interface().(Type), nil
}

func (r *Scheme) MustRegister(prototype any, version string) {
	t := reflect.TypeOf(prototype)
	if t.Kind() != reflect.Pointer {
		panic("All types must be pointers to structs.")
	}
	t = t.Elem()
	r.MustRegisterWithAlias(prototype, NewUngroupedVersionedType(t.Name(), version))
}

func (r *Scheme) TypeForPrototype(prototype any) (Type, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for typ, proto := range r.types {
		// if there is an unversioned type registered, do not use it
		// TODO find a way to avoid this or to fallback to the fully qualified type instead of unqualified ones
		if !typ.HasVersion() {
			continue
		}
		if reflect.TypeOf(prototype).Elem() == reflect.TypeOf(proto).Elem() {
			return typ, nil
		}
	}

	return Type{}, fmt.Errorf("prototype not found in registry")
}

func (r *Scheme) MustTypeForPrototype(prototype any) Type {
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

func (r *Scheme) MustRegisterWithAlias(prototype any, types ...Type) {
	if err := r.RegisterWithAlias(prototype, types...); err != nil {
		panic(err)
	}
}

// NewObject creates a new instance of types.Typed.
func (r *Scheme) NewObject(typ Type) (any, error) {
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

		return object, nil
	}

	if r.allowUnknown {
		return &Raw{}, nil
	}

	return nil, fmt.Errorf("unsupported type: %s", typ)
}

func (r *Scheme) Decode(data io.Reader, into any) error {
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

func (r *Scheme) Convert(from any, into any) error {
	// check if typed is a raw, yaml unmarshalling has its own reflection check so we don't need to do this
	// before the raw assertion.
	if raw, ok := from.(*Raw); ok {
		if _, err := r.TypeForPrototype(into); err != nil && !r.allowUnknown {
			return fmt.Errorf("%T is not a valid registered type and cannot be decoded: %w", into, err)
		}
		fromType, err := GetTypeFromAny(from)
		if err != nil {
			return fmt.Errorf("could not get type from prototype: %w", err)
		}
		if !r.IsRegistered(fromType) {
			return fmt.Errorf("cannot decode from unregistered type: %s", fromType)
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
