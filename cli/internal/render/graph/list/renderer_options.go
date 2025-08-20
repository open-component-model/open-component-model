package list

import (
	"cmp"

	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/cli/internal/render"
)

// RendererOptions defines the options for the list Renderer.
type RendererOptions[T cmp.Ordered] struct {
	// The VertexMarshaller converts a vertex to an object that is expected to
	// be a serializable type (e.g., a struct or map). The VertexMarshaller MUST
	// perform READ-ONLY access to the vertex and its attributes.
	VertexMarshaller VertexMarshaller[T]
	// OutputFormat specifies the format in which the output should be rendered.
	OutputFormat render.OutputFormat
}

// RendererOption is a function that modifies the RendererOptions.
type RendererOption[T cmp.Ordered] func(*RendererOptions[T])

// WithVertexMarshaller sets the VertexSerializer for the Renderer.
func WithVertexMarshaller[T cmp.Ordered](marshaller VertexMarshaller[T]) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.VertexMarshaller = marshaller
	}
}

// WithVertexMarshallerFunc sets the VertexMarshaller based on a function.
func WithVertexMarshallerFunc[T cmp.Ordered](marshallerFunc func(*syncdag.Vertex[T]) (any, error)) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.VertexMarshaller = VertexMarshallerFunc[T](marshallerFunc)
	}
}

// WithOutputFormat sets the output format for the Renderer.
func WithOutputFormat[T cmp.Ordered](format render.OutputFormat) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.OutputFormat = format
	}
}
