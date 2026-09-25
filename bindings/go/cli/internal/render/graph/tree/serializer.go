package tree

import (
	"cmp"
	"fmt"

	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Row represents a single rendered row of the tree table.
//
// A Row may contain Children. Children are rendered as rows nested below the
// Row, before the rows of the graph children of the same vertex. This lets a
// VertexSerializer expose the elements contained in a vertex (for example the
// resources of a component version, or the labels of a resource) at any
// nesting depth.
type Row struct {
	// Cells holds the column values of the row, without the NESTING column.
	// The number and the order of the cells MUST match the header of the
	// Renderer. See [WithHeader].
	Cells []string
	// Children are rows nested below this row. They do not correspond to
	// vertices of the graph.
	Children []Row
}

// VertexSerializer is an interface that defines a method to serialize a vertex.
// The returned Row may contain nested Children rows.
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
	return Row{Cells: []string{
		component.Component.Name,
		component.Component.Version,
		component.Component.Provider.Name,
		component.Component.ToIdentity().String(),
	}}, nil
}
