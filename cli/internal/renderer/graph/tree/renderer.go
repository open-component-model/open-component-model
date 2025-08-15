package tree

import (
	"cmp"
	"context"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/list"

	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/cli/internal/renderer/graph"
)

type TreeRendererOptions[T cmp.Ordered] struct {
	// VertexSerializer is a function that serializes a vertex to a string.
	VertexSerializer func(*syncdag.Vertex[T]) string
}

type TreeRendererOption[T cmp.Ordered] func(*TreeRendererOptions[T])

func WithVertexSerializer[T cmp.Ordered](serializer func(*syncdag.Vertex[T]) string) TreeRendererOption[T] {
	return func(opts *TreeRendererOptions[T]) {
		opts.VertexSerializer = serializer
	}
}

// TreeRenderer renders a tree structure from a DirectedAcyclicGraph.
type TreeRenderer[T cmp.Ordered] struct {
	// The listWriter is used to write the tree structure. It holds manages
	// the indentation and style of the output.
	listWriter list.Writer
	// The vertexSerializer is a function that serializes a vertex to a string.
	// It MUST perform READ-ONLY access to the vertex and its attributes.
	vertexSerializer func(*syncdag.Vertex[T]) string
	// The root ID of the tree to render.
	root T
	// The dag from which the tree is rendered.
	dag *syncdag.DirectedAcyclicGraph[T]
}

// NewTreeRenderer creates a new TreeRenderer for the given DirectedAcyclicGraph.
func NewTreeRenderer[T cmp.Ordered](dag *syncdag.DirectedAcyclicGraph[T], root T, opts ...TreeRendererOption[T]) *TreeRenderer[T] {
	options := &TreeRendererOptions[T]{}
	for _, opt := range opts {
		opt(options)
	}

	if options.VertexSerializer == nil {
		options.VertexSerializer = func(v *syncdag.Vertex[T]) string {
			// Default serializer just returns the vertex ID.
			// This is supposed to be overridden by the user to provide a
			// meaningful representation.
			return fmt.Sprintf("%v", v.ID)
		}
	}
	return &TreeRenderer[T]{
		listWriter:       list.NewWriter(),
		vertexSerializer: options.VertexSerializer,
		root:             root,
		dag:              dag,
	}
}

// Render renders the tree structure starting from the root ID.
// It writes the output to the provided writer.
func (t *TreeRenderer[T]) Render(ctx context.Context, writer io.Writer) error {
	t.listWriter.SetStyle(list.StyleConnectedRounded)
	defer t.listWriter.Reset()

	var zero T
	if t.root == zero {
		return fmt.Errorf("root ID is not set")
	}

	_, exists := t.dag.GetVertex(t.root)
	if !exists {
		return fmt.Errorf("vertex for rootID %v does not exist", t.root)
	}

	if err := t.traverseGraph(ctx, t.root); err != nil {
		return fmt.Errorf("failed to traverse graph: %w", err)
	}
	t.listWriter.SetOutputMirror(writer)
	t.listWriter.Render()
	return nil
}

func (t *TreeRenderer[T]) traverseGraph(ctx context.Context, nodeId T) error {
	vertex, ok := t.dag.GetVertex(nodeId)
	if !ok {
		return fmt.Errorf("vertex for nodeId %v does not exist", nodeId)
	}
	item := t.vertexSerializer(vertex)
	t.listWriter.AppendItem(item)

	// Get children and sort them for stable output
	children := graph.GetNeighborsSorted(ctx, vertex)

	for _, child := range children {
		t.listWriter.Indent()
		if err := t.traverseGraph(ctx, child); err != nil {
			return err
		}
		t.listWriter.UnIndent()
	}
	return nil
}
