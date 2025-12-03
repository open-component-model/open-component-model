package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"
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
	aliases      map[Type]Type
	defaults     map[Type]Typed
}

// GetTypes returns a map of all registered types.
// The keys are the default types, and the values are slices containing all aliases for that type.
// If a type has no aliases, it will have an empty slice as its value.
// The slices of aliases are always sorted for consistent ordering.
func (r *Scheme) GetTypes() map[Type][]Type {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make(map[Type][]Type, len(r.defaults))
	// process aliases first
	for alias, def := range r.aliases {
		types[def] = append(types[def], alias)
		// ensure the slice is always sorted for comparability.
		slices.SortFunc(types[def], func(a, b Type) int {
			return strings.Compare(a.String(), b.String())
		})
	}
	// if there are any types left with no aliases, add them with an empty slice
	for def := range r.defaults {
		if _, exists := types[def]; !exists {
			types[def] = nil
		}
	}
	return types
}

// NewScheme creates a new registry.
func NewScheme(opts ...SchemeOption) *Scheme {
	reg := &Scheme{
		defaults: make(map[Type]Typed),
		aliases:  make(map[Type]Type),
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
	maps.Copy(clone.defaults, r.defaults)
	maps.Copy(clone.aliases, r.aliases)
	return clone
}

// RegisterSchemes calls RegisterScheme for each scheme in the list, registering all types from each scheme.
// Conflicts between Scheme's passed will result in an error on the first conflict found.
// Registration might still have occurred for some types before the error is returned.
func (r *Scheme) RegisterSchemes(schemes ...*Scheme) error {
	for _, scheme := range schemes {
		if err := r.RegisterScheme(scheme); err != nil {
			return err
		}
	}

	return nil
}

// RegisterScheme adds all types from the given scheme to the given scheme, and fails if any of the types already exist.
func (r *Scheme) RegisterScheme(scheme *Scheme) error {
	if scheme == nil {
		return nil
	}

	// Register each type from the source scheme
	for typ := range scheme.defaults {
		if err := r.RegisterSchemeType(scheme, typ); err != nil {
			return err
		}
	}

	return nil
}

func (r *Scheme) MustRegisterScheme(scheme *Scheme) {
	if err := r.RegisterScheme(scheme); err != nil {
		panic(err)
	}
}

// RegisterSchemeType adds a single type from the given scheme to the current scheme
func (r *Scheme) RegisterSchemeType(scheme *Scheme, typ Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if scheme == nil {
		return fmt.Errorf("cannot add to nil scheme")
	}

	if _, exists := r.defaults[typ]; exists {
		return TypeAlreadyRegisteredError(typ)
	}

	prototype, ok := scheme.defaults[typ]
	if !ok {
		return fmt.Errorf("type %q not found in the provided scheme", typ)
	}

	r.defaults[typ] = prototype

	// now copy aliases if they exist
	for alias, forTyp := range scheme.aliases {
		if forTyp.Equal(typ) {
			if _, exists := r.aliases[alias]; exists {
				return fmt.Errorf("%w: cannot register for type %q", TypeAlreadyRegisteredError(alias), typ)
			}
			r.aliases[alias] = typ
		}
	}

	return nil
}

// TypeAlreadyRegisteredError is returned when a type is already registered in the scheme.
// It contains the type that was attempted to be registered.
//
// Use IsTypeAlreadyRegisteredError to check for this error type.
type TypeAlreadyRegisteredError Type

func (e TypeAlreadyRegisteredError) Error() string {
	return fmt.Sprintf("type %q is already registered", Type(e))
}

// IsTypeAlreadyRegisteredError checks if the error is of type TypeAlreadyRegisteredError.
func IsTypeAlreadyRegisteredError(err error) bool {
	if err == nil {
		return false
	}
	return errors.As(err, new(TypeAlreadyRegisteredError))
}

// RegisterWithAlias registers a new type with the registry.
// The first type is the default type and all other types are aliases.
// Note that if Scheme.RegisterWithAlias or Scheme.MustRegister were called before,
// even the first type will be counted as an alias.
func (r *Scheme) RegisterWithAlias(prototype Typed, types ...Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, typ := range types {
		if prototype, exists := r.defaults[typ]; exists {
			return fmt.Errorf("%w: as default for %T", TypeAlreadyRegisteredError(typ), prototype)
		}
		if def, ok := r.aliases[typ]; ok {
			return fmt.Errorf("%w: as alias for %q", TypeAlreadyRegisteredError(typ), def)
		}
		if i == 0 {
			// first type is the def type
			r.defaults[typ] = prototype
		} else {
			// all other types are aliases
			r.aliases[typ] = types[0]
		}
	}
	return nil
}

func (r *Scheme) MustRegister(prototype Typed, version string) {
	t := reflect.TypeOf(prototype)
	if t.Kind() != reflect.Pointer {
		panic("All types must be pointers to structs.")
	}
	t = t.Elem()
	r.MustRegisterWithAlias(prototype, NewVersionedType(t.Name(), version))
}

func (r *Scheme) TypeForPrototype(prototype any) (Type, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for typ, proto := range r.defaults {
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

	_, exists := r.defaults[typ]
	if exists {
		return true
	}

	// check if the type is an alias
	_, exists = r.aliases[typ]

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

	// construct by full type if present in defaults
	if proto, exists := r.defaults[typ]; exists {
		instance := proto.DeepCopyTyped()
		instance.SetType(typ)
		return instance, nil
	}
	// construct by alias if present
	if def, ok := r.aliases[typ]; ok {
		instance := r.defaults[def].DeepCopyTyped()
		instance.SetType(typ)
		return instance, nil
	}

	if r.allowUnknown {
		return &Raw{}, nil
	}

	return nil, fmt.Errorf("unsupported type: %s", typ)
}

func (r *Scheme) Decode(data io.Reader, into Typed) error {
	if _, err := r.TypeForPrototype(into); err != nil {
		if !r.allowUnknown {
			return fmt.Errorf("%T is not a valid registered type and cannot be decoded: %w", into, err)
		}
	}
	oldType := into.GetType()
	bytes, err := io.ReadAll(data)
	if err != nil {
		return fmt.Errorf("could not read data: %w", err)
	}
	if len(bytes) == 0 {
		return fmt.Errorf("cannot decode empty input data")
	}
	if err := yaml.Unmarshal(bytes, into); err != nil {
		return fmt.Errorf("failed to unmarshal raw: %w", err)
	}
	if !oldType.IsEmpty() && !oldType.Equal(into.GetType()) {
		return fmt.Errorf("expected type %q after decoding but got %q", oldType, into.GetType())
	}
	return nil
}

// DefaultType sets the type of the Typed object to its registered type.
// It returns true if the type was updated or an error in case an unknown type was found but
// unknown types are forbidden.
func (r *Scheme) DefaultType(typed Typed) (updated bool, err error) {
	typ := typed.GetType()

	fromType, err := r.TypeForPrototype(typed)
	if err != nil {
		if !r.allowUnknown {
			return false, fmt.Errorf("%T is not a valid registered type and cannot be defaulted: %w", typed, err)
		}
		return false, nil
	}

	if typ.IsEmpty() || !r.IsRegistered(typ) {
		typed.SetType(fromType)
		return true, nil
	}

	return false, nil
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
	// Check for nil arguments.
	if from == nil || into == nil {
		return fmt.Errorf("both 'from' and 'into' must be non-nil")
	}

	// Ensure that from's type is populated. If its not, attempt to infer type information based on the scheme.
	if from.GetType().IsEmpty() {
		// avoid mutating the original object
		from = from.DeepCopyTyped()
		typ, err := r.TypeForPrototype(from)
		if err != nil && !r.allowUnknown {
			return fmt.Errorf("cannot convert from unregistered type: %w", err)
		}
		from.SetType(typ)
	}
	fromType := from.GetType()

	// Case 1: Raw -> Raw or Raw -> Typed
	if rawFrom, ok := from.(*Raw); ok {
		// Raw → Raw: Deep copy the underlying data.
		if rawInto, ok := into.(*Raw); ok {
			rawFrom.DeepCopyInto(rawInto)
			return nil
		}

		// Raw → Typed: Unmarshal the Raw.Data into the target.
		if !r.IsRegistered(fromType) && !r.allowUnknown {
			return fmt.Errorf("cannot decode from unregistered type: %s", fromType)
		}
		if err := json.Unmarshal(rawFrom.Data, into); err != nil {
			return fmt.Errorf("failed to unmarshal from raw: %w", err)
		}
		return nil
	}

	// Case 2: Typed -> Raw
	if rawInto, ok := into.(*Raw); ok {
		if !r.IsRegistered(fromType) && !r.allowUnknown {
			return fmt.Errorf("cannot encode from unregistered type: %s", fromType)
		}
		data, err := json.Marshal(from)
		if err != nil {
			return fmt.Errorf("failed to marshal into raw: %w", err)
		}
		canonicalData, err := jsoncanonicalizer.Transform(data)
		if err != nil {
			return fmt.Errorf("could not canonicalize data: %w", err)
		}
		rawInto.Type = fromType
		rawInto.Data = canonicalData
		return nil
	}

	// Case 3: Generic Typed -> Typed conversion using reflection.
	intoVal := reflect.ValueOf(into)
	if intoVal.Kind() != reflect.Ptr || intoVal.IsNil() {
		return fmt.Errorf("'into' must be a non-nil pointer")
	}
	copied := from.DeepCopyTyped()
	copiedVal := reflect.ValueOf(copied)
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
