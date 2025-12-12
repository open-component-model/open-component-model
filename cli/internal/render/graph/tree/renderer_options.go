package tree

import (
	"cmp"

	"github.com/jedib0t/go-pretty/v6/table"
)

// RendererOptions defines the options for the tree Renderer.
type RendererOptions[T cmp.Ordered] struct {
	// VertexSerializer serializes a vertex into a Row.
	VertexSerializer VertexSerializer[T]
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
