package tree

import (
	"cmp"

	"ocm.software/open-component-model/bindings/go/dag"
)

// WithVertexSerializer sets the VertexSerializer for the Renderer.
func WithVertexSerializer[T cmp.Ordered](serializer VertexSerializer[T]) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.VertexSerializer = serializer
	}
}

// WithVertexSerializerFunc sets the VertexSerializer based on a function.
func WithVertexSerializerFunc[T cmp.Ordered](serializerFunc func(*dag.Vertex[T]) (Row, error)) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.VertexSerializer = VertexSerializerFunc[T](serializerFunc)
	}
}
