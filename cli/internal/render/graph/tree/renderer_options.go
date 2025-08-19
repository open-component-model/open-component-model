package tree

import (
	"cmp"

	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
)

// RendererOptions defines the options for the tree renderer.
type RendererOptions[T cmp.Ordered] struct {
	// VertexSerializer serializes a vertex to a string.
	VertexSerializer VertexSerializer[T]
}

// RendererOption is a function that modifies the RendererOptions.
type RendererOption[T cmp.Ordered] func(*RendererOptions[T])

// WithVertexSerializer sets the VertexSerializer for the renderer.
func WithVertexSerializer[T cmp.Ordered](serializer VertexSerializer[T]) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.VertexSerializer = serializer
	}
}

// WithVertexSerializerFunc sets the VertexSerializer based on a function.
func WithVertexSerializerFunc[T cmp.Ordered](serializerFunc func(*syncdag.Vertex[T]) string) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.VertexSerializer = VertexSerializerFunc[T](serializerFunc)
	}
}
