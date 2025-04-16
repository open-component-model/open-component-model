package registry

import (
	"ocm.software/open-component-model/bindings/go/constructor/input"
	"ocm.software/open-component-model/bindings/go/constructor/input/file"
	spec "ocm.software/open-component-model/bindings/go/constructor/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/constructor/spec/input/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func init() {
	Default.MustRegisterMethod(&v1.File{}, &file.Method{})
}

var Default = New(spec.Scheme)

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
