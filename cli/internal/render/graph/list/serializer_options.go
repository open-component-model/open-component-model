package list

import (
	"cmp"

	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/cli/internal/render"
)

// SerializerOption is a function that modifies the SerializerOptions.
type SerializerOption[T cmp.Ordered] func(*SerializerOptions[T])

// SerializerOptions defines the options for the list Serializer.
type SerializerOptions[T cmp.Ordered] struct {
	// The VertexSerializer converts a vertex to an object that is expected to
	// be a serializable type (e.g., a struct or map). The VertexSerializer MUST
	// perform READ-ONLY access to the vertex and its attributes.
	VertexSerializer VertexSerializer[T]
	// OutputFormat specifies the format in which the output should be rendered.
	OutputFormat render.OutputFormat
}

// WithVertexSerializer sets the VertexSerializer for the Renderer.
func WithVertexSerializer[T cmp.Ordered](serializer VertexSerializer[T]) SerializerOption[T] {
	return func(opts *SerializerOptions[T]) {
		opts.VertexSerializer = serializer
	}
}

// WithVertexSerializerFunc sets the VertexSerializer based on a function.
func WithVertexSerializerFunc[T cmp.Ordered](serializerFunc func(vertex *syncdag.Vertex[T]) (any, error)) SerializerOption[T] {
	return func(opts *SerializerOptions[T]) {
		opts.VertexSerializer = VertexSerializerFunc[T](serializerFunc)
	}
}

// WithOutputFormat sets the output format for the Serializer.
func WithOutputFormat[T cmp.Ordered](format render.OutputFormat) SerializerOption[T] {
	return func(opts *SerializerOptions[T]) {
		opts.OutputFormat = format
	}
}
