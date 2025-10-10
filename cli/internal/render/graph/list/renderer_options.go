package list

import (
	"cmp"
	"io"

	"ocm.software/open-component-model/bindings/go/dag"
)

// RendererOptions defines the options for the list Renderer.
type RendererOptions[T cmp.Ordered] struct {
	// The ListSerializer converts a vertex to an object that is expected to
	// be a serializable type (e.g., a struct or map). The ListSerializer MUST
	// perform READ-ONLY access to the vertex and its attributes.
	ListSerializer ListSerializer[T]
	// Roots are the root vertices of the list to render.
	Roots []T
}

// RendererOption is a function that modifies the RendererOptions.
type RendererOption[T cmp.Ordered] func(*RendererOptions[T])

// WithListSerializer sets the ListSerializer for the Renderer.
func WithListSerializer[T cmp.Ordered](serializer ListSerializer[T]) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.ListSerializer = serializer
	}
}

// WithListSerializerFunc sets the ListSerializer based on a function.
func WithListSerializerFunc[T cmp.Ordered](serializerFunc func(writer io.Writer, vertices []*dag.Vertex[T]) error) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.ListSerializer = ListSerializerFunc[T](serializerFunc)
	}
}

// WithRoots sets the roots for the Renderer.
func WithRoots[T cmp.Ordered](roots ...T) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.Roots = roots
	}
}
