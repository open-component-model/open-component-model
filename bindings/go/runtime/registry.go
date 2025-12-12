package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"reflect"
	"slices"
	"sync"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/runtime/internal/bimap"
)

// Scheme is a dynamic registry for Typed types.
type Scheme struct {
	mu sync.RWMutex
	// allowUnknown allows unknown types to be created.
	// if the constructors cannot determine a match,
	// this will trigger the creation of an unstructured.Unstructured with NewScheme instead of failing.
	allowUnknown bool
	// defaults maps default Types to their reflect.Type.
	// the reflect.Type is the authoritative type for the default Type.
	defaults *bimap.Map[Type, reflect.Type]
	// aliases maps alias Types to their default Type.
	// they are alternative forms of the default Type.
	aliases map[Type]Type
	// instances maps reflect.Type to their prototype instance.
	// this avoids the need to create new instances via reflection and allows
	// passing pre-initialized default values if needed.
	instances map[reflect.Type]Typed
}

// NewScheme creates a new registry.
func NewScheme(opts ...SchemeOption) *Scheme {
	reg := &Scheme{
		defaults:  bimap.New[Type, reflect.Type](),
		aliases:   map[Type]Type{},
		instances: make(map[reflect.Type]Typed),
	}
	for _, opt := range opts {
		opt(reg)
	}
	return reg
}

// GetTypes returns a map of all registered types, where the keys are the default types
// and the values are slices containing all aliases for that type, sorted lexicographically.
func (r *Scheme) GetTypes() map[Type][]Type {
	result := map[Type][]Type{}
	for k, v := range r.GetTypesIter() {
		result[k] = slices.SortedFunc(v, CompareTypesLexicographically)
	}
	return result
}

// GetTypesIter returns an iterator of all registered types.
// The keys are the default types, and the values are iterators containing all aliases for that type.
// If a type has no aliases, it will have an empty slice as its value.
// The slices of aliases are always sorted for consistent ordering.
func (r *Scheme) GetTypesIter() iter.Seq2[Type, iter.Seq[Type]] {
	return func(yield func(Type, iter.Seq[Type]) bool) {
		r.mu.RLock()
		defer r.mu.RUnlock()
		for typ := range r.defaults.Iter() {
			if !yield(typ, r.AliasesIter(typ)) {
				return
			}
		}
	}
}

// AliasesIter returns an iterator of all aliases for the given type.
func (r *Scheme) AliasesIter(typ Type) iter.Seq[Type] {
	return func(yield func(Type) bool) {
		r.mu.RLock()
		defer r.mu.RUnlock()
		for alias, forTyp := range r.aliases {
			if !forTyp.Equal(typ) {
				continue
			}
			if !yield(alias) {
				return
			}
		}
	}
}

// RegisterSchemeType registers a given type from another scheme into this scheme.
// It registers the type as well as all its aliases.
func (r *Scheme) RegisterSchemeType(scheme *Scheme, typ Type) error {
	if scheme == nil {
		return errors.New("cannot register type from nil scheme")
	}

	scheme.mu.RLock()
	defer scheme.mu.RUnlock()

	// check if the type is registered as default type in the source scheme
	// or get its default type if it's an alias
	rt, exists := scheme.defaults.GetLeft(typ)
	if !exists {
		if aliasFor, ok := scheme.aliases[typ]; ok {
			rt, _ = scheme.defaults.GetLeft(aliasFor)
		} else {
			return fmt.Errorf("type %q is not registered in the scheme", typ)
		}
	}

	// register the type in the new scheme
	return r.RegisterWithAlias(
		// first the default type
		scheme.instances[rt],
		// then the default type and all its aliases
		append([]Type{typ}, slices.Collect(scheme.AliasesIter(typ))...)...,
	)
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
	clone.defaults = r.defaults.Clone()
	maps.Copy(clone.aliases, r.aliases)
	maps.Copy(clone.instances, r.instances)
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

	scheme.mu.RLock()
	defer scheme.mu.RUnlock()

	// Register each type from the source scheme
	for defaultType := range scheme.defaults.Iter() {
		if err := r.RegisterSchemeType(scheme, defaultType); err != nil {
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
	if len(types) == 0 {
		return fmt.Errorf("no types provided to register %T", prototype)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	rt := reflect.TypeOf(prototype).Elem()
	defaultTyp, defaultExists := r.defaults.GetRight(rt)
	if !defaultExists {
		r.defaults.Set(types[0], rt)
		r.instances[rt] = prototype.DeepCopyTyped()
		defaultTyp = types[0]
		types = types[1:]
	}
	for _, typ := range types {
		if _, exists := r.defaults.GetLeft(typ); exists {
			return fmt.Errorf("%w: as default for %T", TypeAlreadyRegisteredError(typ), prototype)
		}
		if def, ok := r.aliases[typ]; ok {
			return fmt.Errorf("%w: as alias for %q", TypeAlreadyRegisteredError(typ), def)
		}
		r.aliases[typ] = defaultTyp
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

	for typ, proto := range r.defaults.Iter() {
		if reflect.TypeOf(prototype).Elem() == proto {
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

	_, exists := r.defaults.GetLeft(typ)
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
	if proto, exists := r.defaults.GetLeft(typ); exists {
		instance := r.instances[proto].DeepCopyTyped()
		instance.SetType(typ)
		return instance, nil
	}
	// construct by alias if present
	if def, ok := r.aliases[typ]; ok {
		rt, _ := r.defaults.GetLeft(def)
		instance := r.instances[rt].DeepCopyTyped()
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
