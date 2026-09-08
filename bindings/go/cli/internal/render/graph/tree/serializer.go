package tree

import (
	"cmp"
	"fmt"

	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Row represents a single rendered row
type Row struct {
	Component string
	Version   string
	Provider  string
	Identity  string
}

// VertexSerializer is an interface that defines a method to serialize a vertex.
type VertexSerializer[T cmp.Ordered] interface {
	Serialize(*dag.Vertex[T]) (Row, error)
}

type VertexSerializerFunc[T cmp.Ordered] func(*dag.Vertex[T]) (Row, error)

func (f VertexSerializerFunc[T]) Serialize(v *dag.Vertex[T]) (Row, error) {
	return f(v)
}

func defaultVertexSerializer[T cmp.Ordered](vertex *dag.Vertex[T]) (Row, error) {
	untypedComponent, ok := vertex.Attributes[syncdag.AttributeValue]
	if !ok {
		return Row{}, fmt.Errorf("vertex %v does not have a %s attribute", vertex.ID, syncdag.AttributeValue)
	}
	component, ok := untypedComponent.(*descruntime.Descriptor)
	if !ok {
		return Row{}, fmt.Errorf("vertex %v has a value attribute of unexpected type %T, expected type %T", vertex.ID, untypedComponent, &descruntime.Descriptor{})
	}
	return Row{
		Component: component.Component.Name,
		Version:   component.Component.Version,
		Provider:  component.Component.Provider.Name,
		Identity:  component.Component.ToIdentity().String(),
	}, nil
}
