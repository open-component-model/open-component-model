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

type RendererOptions[T cmp.Ordered] struct {
	// VertexSerializer is a function that serializes a vertex to a string.
	VertexSerializer VertexSerializer[T]
}

type RendererOption[T cmp.Ordered] func(*RendererOptions[T])

func WithVertexSerializer[T cmp.Ordered](serializer VertexSerializer[T]) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.VertexSerializer = serializer
	}
}

func WithVertexSerializerFunc[T cmp.Ordered](serializerFunc func(*syncdag.Vertex[T]) string) RendererOption[T] {
	return func(opts *RendererOptions[T]) {
		opts.VertexSerializer = VertexSerializerFunc[T](serializerFunc)
	}
}

type VertexSerializer[T cmp.Ordered] interface {
	Serialize(*syncdag.Vertex[T]) string
}

type VertexSerializerFunc[T cmp.Ordered] func(*syncdag.Vertex[T]) string

func (f VertexSerializerFunc[T]) Serialize(v *syncdag.Vertex[T]) string {
	return f(v)
}

// Renderer renders a tree structure from a DirectedAcyclicGraph.
type Renderer[T cmp.Ordered] struct {
	// The listWriter is used to write the tree structure. It holds manages
	// the indentation and style of the output.
	listWriter list.Writer
	// The vertexSerializer is a function that serializes a vertex to a string.
	// It MUST perform READ-ONLY access to the vertex and its attributes.
	vertexSerializer VertexSerializer[T]
	// The root ID of the tree to render.
	root T
	// The dag from which the tree is rendered.
	dag *syncdag.DirectedAcyclicGraph[T]
}

// New creates a new TreeRenderer for the given DirectedAcyclicGraph.
func New[T cmp.Ordered](dag *syncdag.DirectedAcyclicGraph[T], root T, opts ...RendererOption[T]) *Renderer[T] {
	options := &RendererOptions[T]{}
	for _, opt := range opts {
		opt(options)
	}

	if options.VertexSerializer == nil {
		options.VertexSerializer = VertexSerializerFunc[T](func(v *syncdag.Vertex[T]) string {
			// Default serializer just returns the vertex ID.
			// This is supposed to be overridden by the user to provide a
			// meaningful representation.
			return fmt.Sprintf("%v", v.ID)
		})
	}
	return &Renderer[T]{
		listWriter:       list.NewWriter(),
		vertexSerializer: options.VertexSerializer,
		root:             root,
		dag:              dag,
	}
}

// Render renders the tree structure starting from the root ID.
// It writes the output to the provided writer.
func (t *Renderer[T]) Render(ctx context.Context, writer io.Writer) error {
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

func (t *Renderer[T]) traverseGraph(ctx context.Context, nodeId T) error {
	vertex, ok := t.dag.GetVertex(nodeId)
	if !ok {
		return fmt.Errorf("vertex for nodeId %v does not exist", nodeId)
	}
	item := t.vertexSerializer.Serialize(vertex)
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
