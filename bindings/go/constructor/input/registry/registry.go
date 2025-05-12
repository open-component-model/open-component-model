package registry

import (
	"ocm.software/open-component-model/bindings/go/constructor/input"
	"ocm.software/open-component-model/bindings/go/constructor/input/file"
	filev1 "ocm.software/open-component-model/bindings/go/constructor/input/file/spec/v1"
	"ocm.software/open-component-model/bindings/go/constructor/input/helm"
	helmv1 "ocm.software/open-component-model/bindings/go/constructor/input/helm/spec/v1"
	"ocm.software/open-component-model/bindings/go/constructor/input/utf8"
	"ocm.software/open-component-model/bindings/go/constructor/input/utf8/spec/v2alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	// Scheme is the default runtime scheme for the registry
	Scheme = runtime.NewScheme()
	// Default is the default registry instance using the default scheme
	Default = New(Scheme)
)

// MustAddToScheme registers the file and UTF8 types with the given scheme.
// It registers both versioned and unversioned types.
func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&filev1.File{},
		runtime.NewVersionedType("file", filev1.Version),
		runtime.NewUnversionedType("file"),
	)

	scheme.MustRegisterWithAlias(&v2alpha1.UTF8{},
		runtime.NewVersionedType("utf8", v2alpha1.Version),
		runtime.NewUnversionedType("utf8"),
	)

	scheme.MustRegisterWithAlias(&helmv1.Helm{},
		runtime.NewVersionedType("helmChart", helmv1.Version),
		runtime.NewUnversionedType("helmChart"),
		runtime.NewVersionedType("helm", helmv1.Version),
		runtime.NewUnversionedType("helm"),
	)
}

func init() {
	MustAddToScheme(Scheme)
	Default.MustRegisterMethod(&filev1.File{}, &file.Method{Scheme: Scheme})
	Default.MustRegisterMethod(&v2alpha1.UTF8{}, &utf8.Method{Scheme: Scheme})
	Default.MustRegisterMethod(&helmv1.Helm{}, &helm.Method{Scheme: Scheme})
}

// Registry manages resource input methods for different types
type Registry struct {
	methods map[runtime.Type]input.ResourceInputMethod
	scheme  *runtime.Scheme
}

// New creates a new Registry instance with the given scheme
func New(scheme *runtime.Scheme) *Registry {
	return &Registry{
		scheme:  scheme,
		methods: make(map[runtime.Type]input.ResourceInputMethod),
	}
}

// MustRegisterMethod registers a resource input method for a given prototype type.
// Panics if the registration fails.
func (r *Registry) MustRegisterMethod(prototype runtime.Typed, method input.ResourceInputMethod) {
	r.methods[r.scheme.MustTypeForPrototype(prototype)] = method
}

// GetFor retrieves the resource input method for a given typed object.
// Returns the method and a boolean indicating if the method was found.
func (r *Registry) GetFor(t runtime.Typed) (input.ResourceInputMethod, bool) {
	typed, err := r.scheme.NewObject(t.GetType())
	if err != nil {
		return nil, false
	}
	typ, err := r.scheme.TypeForPrototype(typed)
	if err != nil {
		return nil, false
	}
	method, ok := r.methods[typ]
	return method, ok
}
