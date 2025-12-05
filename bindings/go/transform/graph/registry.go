package graph

import (
	"context"

	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/transformations"
)

type GenericTransformation interface {
	Transform(
		ctx context.Context,
		step *v1alpha1.GenericTransformation,
		credentialProvider credentials.Resolver,
	) (*v1alpha1.GenericTransformation, error)
	GetDeclType() *stv6jsonschema.DeclType
}

type Registry struct {
	// runtime.Type is the transformation type (e.g. component.download.oci)
	// transformations map[runtime.Type]Transformation
	// holds n instances of e.g. ComponentVersionDownloadTransformation
	// 1 instance per plugin (oci, ctf)
	transformations map[runtime.Type]GenericTransformation
}

func NewRegistry(ctx context.Context, provider repository.ComponentVersionRepositoryProvider) (*Registry, error) {
	transformation, err := transformations.NewOCIComponentVersionDownloadTransformation(provider)
	if err != nil {
		return nil, err
	}

	reg := &Registry{
		transformations: map[runtime.Type]GenericTransformation{
			transformation.GetType(): transformation,
		},
	}
	return reg, nil
}
