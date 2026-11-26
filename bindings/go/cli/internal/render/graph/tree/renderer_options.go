package tree

import (
	"cmp"

	"github.com/jedib0t/go-pretty/v6/table"
)

// NestingColumn is the heading of the column that shows the tree structure.
// It is always the first column and is managed by the Renderer.
const NestingColumn = "NESTING"

// DefaultHeader is the header used if the Renderer has no explicit header.
// It describes a component version per row.
var DefaultHeader = []string{"COMPONENT", "VERSION", "PROVIDER", "IDENTITY"}

// RendererOptions defines the options for the tree Renderer.
type RendererOptions[T cmp.Ordered] struct {
	// VertexSerializer serializes a vertex into a Row. The Row may carry nested
	// Children rows for elements contained in the vertex.
	VertexSerializer VertexSerializer[T]
	// Header holds the column headings, without the NESTING column. The number
	// and the order of the headings MUST match the cells produced by the
	// VertexSerializer. Defaults to DefaultHeader.
	Header []string
	// Roots are the root vertices of the tree to render.
	Roots []T
	// TableStyle allows customizing the go-pretty table style used by the renderer.
	TableStyle table.Style
	// Headerless suppresses the header row entirely (including the NESTING
	// heading), for a pure label tree with no column titles.
	Headerless bool
	// TreeStyle overrides the NESTING column drawing characters. Nil keeps
	// DefaultTreeStyle.
	TreeStyle *TreeStyle
}

// RendererOption is a function that modifies the RendererOptions.
type RendererOption[T cmp.Ordered] func(*RendererOptions[T])

// WithRoots sets the roots for the Renderer.
func WithRoots[T cmp.Ordered](roots ...T) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.Roots = roots
	}
}

// WithHeader sets the column headings, without the NESTING column.
func WithHeader[T cmp.Ordered](header ...string) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.Header = header
	}
}

// WithoutHeader suppresses the header row entirely, rendering a pure label tree
// with no column headings (not even NESTING).
func WithoutHeader[T cmp.Ordered]() RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.Headerless = true
	}
}

// WithTreeStyle overrides the NESTING column drawing characters (for example to drop
// the child-node indicator for a plain tree).
func WithTreeStyle[T cmp.Ordered](style TreeStyle) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.TreeStyle = &style
	}
}
