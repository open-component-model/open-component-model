package registry

import (
	"ocm.software/open-component-model/bindings/go/constructor/input"
	"ocm.software/open-component-model/bindings/go/constructor/input/file"
	"ocm.software/open-component-model/bindings/go/constructor/input/file/spec/v1"
	"ocm.software/open-component-model/bindings/go/constructor/input/utf8"
	"ocm.software/open-component-model/bindings/go/constructor/input/utf8/spec/v2alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()
var Default = New(Scheme)

func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1.File{}, runtime.NewVersionedType("file", v1.Version))
	scheme.MustRegisterWithAlias(&v1.File{}, runtime.NewUnversionedType("file"))

	scheme.MustRegisterWithAlias(&v2alpha1.UTF8{}, runtime.NewVersionedType("utf8", v2alpha1.Version))
	scheme.MustRegisterWithAlias(&v2alpha1.UTF8{}, runtime.NewUnversionedType("utf8"))
}

func init() {
	MustAddToScheme(Scheme)
	Default.MustRegisterMethod(&v1.File{}, &file.Method{Scheme: Scheme})
	Default.MustRegisterMethod(&v2alpha1.UTF8{}, &utf8.Method{Scheme: Scheme})
}

type Registry struct {
	methods map[runtime.Type]input.Method
	scheme  *runtime.Scheme
}

func New(scheme *runtime.Scheme) *Registry {
	return &Registry{
		scheme:  scheme,
		methods: make(map[runtime.Type]input.Method),
	}
}

func (r *Registry) MustRegisterMethod(prototype runtime.Typed, method input.Method) {
	r.methods[r.scheme.MustTypeForPrototype(prototype)] = method
}

func (r *Registry) GetFor(t runtime.Typed) (input.Method, bool) {
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
