package list

import (
	"cmp"
	"fmt"

	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
)

type OutputFormat int

const (
	OutputFormatJSON OutputFormat = iota
	OutputFormatYAML
	OutputFormatNDJSON
)

func (o OutputFormat) String() string {
	switch o {
	case OutputFormatJSON:
		return "json"
	case OutputFormatYAML:
		return "yaml"
	case OutputFormatNDJSON:
		return "ndjson"
	default:
		return fmt.Sprintf("unknown(%d)", o)
	}
}

type RendererOptions[T cmp.Ordered] struct {
	// VertexMarshalizer converts a vertex to an object of type U. U is expected
	// to be a serializable type (e.g., a struct or map).
	// The marshalizer MUST perform READ-ONLY access to the vertex and its
	// attributes.
	VertexMarshalizer VertexMarshalizer[T]
	// OutputFormat specifies the format in which the output should be rendered.
	OutputFormat OutputFormat
}

type RendererOption[T cmp.Ordered] func(*RendererOptions[T])

func WithVertexMarshalizer[T cmp.Ordered](marshalizer VertexMarshalizer[T]) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.VertexMarshalizer = marshalizer
	}
}

func WithVertexMarshalizerFunc[T cmp.Ordered](marshalizerFunc func(*syncdag.Vertex[T]) (any, error)) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.VertexMarshalizer = VertexMarshalizerFunc[T](marshalizerFunc)
	}
}

func WithOutputFormat[T cmp.Ordered](format OutputFormat) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.OutputFormat = format
	}
}
