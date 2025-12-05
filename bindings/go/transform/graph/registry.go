package graph

import (
	"bytes"
	"context"

	"github.com/santhosh-tekuri/jsonschema/v6"
	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/transformations"
)

type Transformation interface {
	Transform(step *runtime.Unstructured) (*runtime.Unstructured, error)
}

type Registry struct {
	// runtime.Type is the transformation type (e.g. component.download.oci)
	transformations map[runtime.Type]Transformation
	// transformations map[runtime.Type]Transformation
	// holds n instances of e.g. ComponentVersionDownloadTransformation
	// 1 instance per plugin (oci, ctf)
}

func NewRegistry(ctx context.Context, provider repository.ComponentVersionRepositoryProvider) (*Registry, error) {
	rawSchema := oci.Repository{}.JSONSchema()
	schema := stv6jsonschema.Schema{}
	un, err := jsonschema.UnmarshalJSON(bytes.NewReader(rawSchema))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.AddResource()

	reg := &Registry{
		transformations: map[runtime.Type]Transformation{
			"component.download.oci": transformations.NewTransformation(provider, jsonschema.NewSchemaDeclType()),
		},
	}
}
