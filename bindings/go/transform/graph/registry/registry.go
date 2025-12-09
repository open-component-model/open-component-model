package registry

import (
	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type GenericTransformation interface {
	GetDeclType() (*stv6jsonschema.DeclType, error)
}

type Registry struct {
	transformations map[runtime.Type]GenericTransformation
	Scheme          *runtime.Scheme
}

func (r Registry) RegisterTransformation(typ runtime.Type, prototype interface {
	runtime.Typed
	GenericTransformation
}) error {
	r.transformations[typ] = prototype
	if err := r.Scheme.RegisterWithAlias(prototype, typ); err != nil {
		return err
	}
	return nil
}

func (r Registry) GetTransformation(typ runtime.Type) (GenericTransformation, bool) {
	transformation, ok := r.transformations[typ]
	return transformation, ok
}

var DefaultRegistry *Registry

func init() {
	DefaultRegistry = NewRegistry()
}

func NewRegistry() *Registry {
	return &Registry{transformations: map[runtime.Type]GenericTransformation{}, Scheme: runtime.NewScheme()}
}
