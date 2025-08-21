package tree

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	"github.com/jedib0t/go-pretty/v6/list"

	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/cli/internal/render/graph"
)

// Renderer renders a tree structure from a DirectedAcyclicGraph.
// The output rendered by the Renderer looks like this:
//
//	── A
//	   ├─ B
//	   │  ╰─ C
//	   ╰─ D
//
// Each letter corresponds to a vertex in the DirectedAcyclicGraph. The concrete
// representation of the vertex is defined by the VertexSerializer.
type Renderer[T cmp.Ordered] struct {
	// The listWriter is used to write the tree structure. It holds manages
	// the indentation and style of the output.
	listWriter list.Writer
	// The VertexSerializer serializes a vertex to a string.
	// It MUST perform READ-ONLY access to the vertex and its attributes.
	vertexSerializer VertexSerializer[T]
	// The roots of the tree to render.
	// The roots are part of the Renderer instead of being passed to the
	// Render method to keep renderer.Renderer decoupled of specific data
	// structures.
	// The roots are optional. If not provided, the Renderer will
	// dynamically determine the roots from the DirectedAcyclicGraph.
	roots []T
	// The dag from which the tree is rendered.
	dag *syncdag.DirectedAcyclicGraph[T]
}

// VertexSerializer is an interface that defines a method to serialize a vertex.
type VertexSerializer[T cmp.Ordered] interface {
	Serialize(*syncdag.Vertex[T]) (string, error)
}

// VertexSerializerFunc is a function type that implements the VertexSerializer
// interface.
type VertexSerializerFunc[T cmp.Ordered] func(*syncdag.Vertex[T]) (string, error)

// Serialize implements the VertexSerializer interface for VertexSerializerFunc.
func (f VertexSerializerFunc[T]) Serialize(v *syncdag.Vertex[T]) (string, error) {
	return f(v)
}

// New creates a new Renderer for the given DirectedAcyclicGraph.
func New[T cmp.Ordered](ctx context.Context, dag *syncdag.DirectedAcyclicGraph[T], opts ...RendererOption[T]) *Renderer[T] {
	options := &RendererOptions[T]{}
	for _, opt := range opts {
		opt(options)
	}

	if options.VertexSerializer == nil {
		options.VertexSerializer = VertexSerializerFunc[T](func(v *syncdag.Vertex[T]) (string, error) {
			// Default serializer just returns the vertex ID.
			// This is supposed to be overridden by the user to provide a
			// meaningful representation.
			return fmt.Sprintf("%v", v.ID), nil
		})
	}

	if len(options.Roots) == 0 {
		slog.DebugContext(ctx, "no roots provided, dynamically determining roots from dag")
	}

	return &Renderer[T]{
		listWriter:       list.NewWriter(),
		vertexSerializer: options.VertexSerializer,
		roots:            options.Roots,
		dag:              dag,
	}
}

// Render renders the tree structure starting from the root ID.
// It writes the output to the provided writer.
func (t *Renderer[T]) Render(ctx context.Context, writer io.Writer) error {
	t.listWriter.SetStyle(list.StyleConnectedRounded)
	defer t.listWriter.Reset()

	roots := t.roots
	if len(roots) == 0 {
		roots = t.dag.Roots()
	} else {
		for index, root := range roots {
			if _, exists := t.dag.GetVertex(root); !exists {
				// If root does not exist in the dag yet, we exclude it from the
				// current rendering run.
				// The root might be added to the graph, after the rendering
				// has started, so we do not want to fail the rendering.
				roots = append(roots[:index], roots[index+1:]...)
			}
		}
	}

	slices.Sort(roots)

	for _, root := range roots {
		if err := t.traverseGraph(ctx, root); err != nil {
			return fmt.Errorf("failed to traverse graph: %w", err)
		}
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
	item, err := t.vertexSerializer.Serialize(vertex)
	if err != nil {
		return fmt.Errorf("failed to serialize vertex %v: %w", vertex.ID, err)
	}
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
