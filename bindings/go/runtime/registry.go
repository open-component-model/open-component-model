package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"reflect"
	"sync"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
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

func (r *Scheme) RegisterWithAlias(prototype Typed, types ...Type) error {
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

func (r *Scheme) MustRegister(prototype Typed, version string) {
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

// NewObject creates a new instance of runtime.Typed.
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

// Convert transforms one Typed object into another. Both 'from' and 'into' must be non-nil pointers.
//
// Special Cases:
//   - Raw → Raw: performs a deep copy of the underlying []byte data.
//   - Raw → Typed: unmarshals Raw.Data JSON via json.Unmarshal into the Typed object (if Typed.GetType is registered).
//   - Typed → Raw: marshals the Typed with json.Marshal, applies canonicalization, and stores the result in Raw.Data.
//     (See Raw.UnmarshalJSON for equivalent behavior)
//   - Typed → Typed: performs a deep copy using Typed.DeepCopyTyped, with reflection-based assignment.
//
// Errors are returned if:
//   - Either argument is nil.
//   - A type is not registered in the Scheme (for Raw conversions).
//   - A reflection-based assignment fails due to type mismatch.
func (r *Scheme) Convert(from Typed, into Typed) error {
	if from == nil || into == nil {
		return fmt.Errorf("both 'from' and 'into' must be non-nil")
	}

	fromType := from.GetType()

	// Handle Raw → Raw (cheap shortcut)
	if rawFrom, ok := from.(*Raw); ok {
		// in case the from type info is empty, we may still infer the actual type from the type in the registry
		if fromType.IsEmpty() {
			typ, err := r.TypeForPrototype(from)
			if err != nil {
				return fmt.Errorf("cannot convert from unregistered type: %w", err)
			}
			fromType = typ
			rawFrom.Type = typ
		}

		if rawInto, ok := into.(*Raw); ok {
			rawFrom.DeepCopyInto(rawInto)
			return nil
		}

		// Raw → Typed (YAML decode)
		if !r.IsRegistered(fromType) {
			return fmt.Errorf("cannot decode from unregistered type: %s", fromType)
		}
		if err := json.Unmarshal(rawFrom.Data, into); err != nil {
			return fmt.Errorf("failed to unmarshal from raw: %w", err)
		}
		return nil
	}

	// Typed → Raw
	if rawInto, ok := into.(*Raw); ok {
		// in case the from type info is empty, we may still infer the actual type from the type in the registry
		if fromType.IsEmpty() {
			typ, err := r.TypeForPrototype(from)
			if err != nil {
				return fmt.Errorf("cannot convert from unregistered type: %w", err)
			}
			fromType = typ
		}
		// 'from' can never be a *Raw (shouldn't happen due to earlier check)

		if !r.IsRegistered(fromType) {
			return fmt.Errorf("cannot encode from unregistered type: %s", fromType)
		}

		data, err := json.Marshal(from)
		if err != nil {
			return fmt.Errorf("failed to marshal into raw: %w", err)
		}

		if data, err = AddTypeIfMissing(data, fromType); err != nil {
			return fmt.Errorf("failed to add type to raw: %w", err)
		}

		canonicalData, err := jsoncanonicalizer.Transform(data)
		if err != nil {
			return fmt.Errorf("could not canonicalize data: %w", err)
		}

		rawInto.Type = fromType
		rawInto.Data = canonicalData
		return nil
	}

	// Generic Typed → Typed conversion
	intoVal := reflect.ValueOf(into)
	if intoVal.Kind() != reflect.Ptr || intoVal.IsNil() {
		return fmt.Errorf("'into' must be a non-nil pointer")
	}

	copied := from.DeepCopyTyped()
	copiedVal := reflect.ValueOf(copied)

	// Make sure copiedVal is the value, not the pointer
	if copiedVal.Kind() == reflect.Ptr {
		copiedVal = copiedVal.Elem()
	}

	intoElem := intoVal.Elem()
	if !copiedVal.Type().AssignableTo(intoElem.Type()) {
		return fmt.Errorf("cannot assign value of type %T to target of type %T", copied, into)
	}

	intoElem.Set(copiedVal)
	return nil
}
