package tree

import (
	"cmp"
	"fmt"

	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const (
	descriptorAttribute = "descriptor"
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
	Serialize(*syncdag.Vertex[T]) (Row, error)
}

type VertexSerializerFunc[T cmp.Ordered] func(*syncdag.Vertex[T]) (Row, error)

func (f VertexSerializerFunc[T]) Serialize(v *syncdag.Vertex[T]) (Row, error) {
	return f(v)
}

func defaultVertexSerializer[T cmp.Ordered]() VertexSerializer[T] {
	return VertexSerializerFunc[T](func(vertex *syncdag.Vertex[T]) (Row, error) {
		if d, ok := vertex.MustGetAttribute(descriptorAttribute).(*descruntime.Descriptor); ok {
			return Row{
				Component: d.Component.Name,
				Version:   d.Component.Version,
				Provider:  d.Component.Provider.Name,
				Identity:  fmt.Sprintf("%v", vertex.ID),
			}, nil
		}
		return Row{}, fmt.Errorf("vertex %v does not have a descriptor attribute", vertex.ID)
	})
}
