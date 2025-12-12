package list

import (
	"cmp"

	"ocm.software/open-component-model/bindings/go/dag"
	"ocm.software/open-component-model/cli/internal/render"
)

// SerializerOption is a function that modifies the SerializerOptions.
type SerializerOption[T cmp.Ordered] func(*Serializer[T])

// WithVertexSerializer sets the VertexSerializer for the Renderer.
func WithVertexSerializer[T cmp.Ordered](serializer VertexSerializer[T]) SerializerOption[T] {
	return func(opts *Serializer[T]) {
		opts.VertexSerializer = serializer
	}
}

// WithVertexSerializerFunc sets the VertexSerializer based on a function.
func WithVertexSerializerFunc[T cmp.Ordered](serializerFunc func(vertex *dag.Vertex[T]) (any, error)) SerializerOption[T] {
	return func(opts *Serializer[T]) {
		opts.VertexSerializer = VertexSerializerFunc[T](serializerFunc)
	}
}

// WithOutputFormat sets the output format for the Serializer.
func WithOutputFormat[T cmp.Ordered](format render.OutputFormat) SerializerOption[T] {
	return func(opts *Serializer[T]) {
		opts.OutputFormat = format
	}
}
