package tree

import (
	"cmp"

	"github.com/jedib0t/go-pretty/v6/table"

	"ocm.software/open-component-model/bindings/go/dag"
)

// RendererOptions defines the options for the tree Renderer.
type RendererOptions[T cmp.Ordered] struct {
	// VertexSerializer serializes a vertex into a Row.
	VertexSerializer VertexSerializer[T]
	// SubRowProvider optionally returns additional leaf rows that are rendered
	// as children of a vertex. It is used, for example, to render the resources
	// contained in a component version below the component node. It must not
	// modify the vertex or its attributes.
	SubRowProvider SubRowProvider[T]
	// Roots are the root vertices of the tree to render.
	Roots []T
	// TableStyle allows customizing the go-pretty table style used by the renderer.
	TableStyle table.Style
}

// RendererOption is a function that modifies the RendererOptions.
type RendererOption[T cmp.Ordered] func(*RendererOptions[T])

// WithRoots sets the roots for the Renderer.
func WithRoots[T cmp.Ordered](roots ...T) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.Roots = roots
	}
}

// WithSubRowProvider sets a provider for additional leaf rows rendered below a
// vertex (for example the resources of a component version).
func WithSubRowProvider[T cmp.Ordered](provider SubRowProvider[T]) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.SubRowProvider = provider
	}
}

func WithSubRowProviderFunc[T cmp.Ordered](fn func(*dag.Vertex[T]) ([]Row, error)) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.SubRowProvider = SubRowProviderFunc[T](fn)
	}
}
