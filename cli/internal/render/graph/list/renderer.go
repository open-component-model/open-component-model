package list

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"

	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/cli/internal/render/graph"
)

// Renderer renders a tree from a DirectedAcyclicGraph as a flat last in a
// particular output format.
// The output rendered by the Renderer with OutputFormatJSON looks like this:
//
//	[
//	  "A",
//	  "B",
//	  "C",
//	  "D"
//	]
//
// The output is analogous to a tree structure, but without the indentation.
//
//	── A
//	   ├─ B
//	   │  ╰─ C
//	   ╰─ D
//
// Each letter corresponds to a vertex in the DirectedAcyclicGraph. The concrete
// representation of the vertex is defined by the ListSerializer.
type Renderer[T cmp.Ordered] struct {
	// The vertices is a slice of vertices that will be rendered.
	vertices []*syncdag.Vertex[T]
	// The ListSerializer converts a vertex to an object that is added to vertices.
	// The returned object is expected to be a serializable type (e.g., a struct
	// or map). The ListSerializer MUST perform READ-ONLY access to the vertex and its
	// attributes.
	listSerializer ListSerializer[T]
	// The roots of the tree to render.
	// The order of the roots determines the order of the root nodes in the
	// rendered output.
	// The roots are part of the Renderer instead of being passed to the
	// Render method to keep renderer.Renderer decoupled of specific data
	// structures.
	// The roots are optional. If not provided, the Renderer will
	// dynamically determine the roots from the DirectedAcyclicGraph.
	roots []T
	// The dag from which the tree is rendered.
	dag *syncdag.DirectedAcyclicGraph[T]
}

// ListSerializer is an interface that defines a method to create a
// serializable object from a vertex.
type ListSerializer[T cmp.Ordered] interface {
	Serialize(writer io.Writer, vertices []*syncdag.Vertex[T]) error
}

// ListSerializerFunc is a function type that implements the ListSerializer
// interface.
type ListSerializerFunc[T cmp.Ordered] func(writer io.Writer, vertices []*syncdag.Vertex[T]) error

// Serialize implements the ListSerializer interface for ListSerializerFunc.
func (f ListSerializerFunc[T]) Serialize(writer io.Writer, vertices []*syncdag.Vertex[T]) error {
	return f(writer, vertices)
}

// New creates a new Renderer for the given DirectedAcyclicGraph.
func New[T cmp.Ordered](ctx context.Context, dag *syncdag.DirectedAcyclicGraph[T], opts ...RendererOption[T]) *Renderer[T] {
	options := &RendererOptions[T]{}
	for _, opt := range opts {
		opt(options)
	}

	if options.ListSerializer == nil {
		options.ListSerializer = ListSerializerFunc[T](func(writer io.Writer, vertices []*syncdag.Vertex[T]) error {
			// Default marshaller just returns the vertex ID.
			// This is supposed to be overridden by the user to provide a
			// meaningful representation.
			var list []string
			for _, vertex := range vertices {
				list = append(list, fmt.Sprintf("%v", vertex.ID))
			}
			data, err := json.MarshalIndent(list, "", "  ")
			if err != nil {
				return fmt.Errorf("encoding vertices as JSON failed: %w", err)
			}
			data = append(data, '\n') // RunRenderLoop expects a newline at the end of the output.
			if _, err = writer.Write(data); err != nil {
				return err
			}
			return nil
		})
	}

	if len(options.Roots) == 0 {
		slog.DebugContext(ctx, "no roots provided, dynamically determining roots from dag")
	}

	return &Renderer[T]{
		vertices:       make([]*syncdag.Vertex[T], 0),
		listSerializer: options.ListSerializer,
		roots:          options.Roots,
		dag:            dag,
	}
}

// Render renders the tree structure starting from the root ID.
// It writes the output to the provided writer.
func (r *Renderer[T]) Render(ctx context.Context, writer io.Writer) error {
	defer func() {
		r.vertices = r.vertices[:0]
	}()

	roots := r.roots
	if len(roots) == 0 {
		roots = r.dag.Roots()
		// We only do this for auto-detected roots. If the roots are provided,
		// we want to preserve the order.
		slices.Sort(roots)
	} else {
		for index, root := range roots {
			if _, exists := r.dag.GetVertex(root); !exists {
				// If root does not exist in the dag yet, we exclude it from the
				// current rendering run.
				// The root might be added to the graph, after the rendering
				// has started, so we do not want to fail the rendering.
				roots = append(roots[:index], roots[index+1:]...)
			}
		}
	}

	for _, root := range roots {
		if err := r.traverseGraph(ctx, root); err != nil {
			return fmt.Errorf("failed to traverse graph: %w", err)
		}
	}
	if err := r.renderObjects(writer); err != nil {
		return err
	}

	return nil
}

func (r *Renderer[T]) traverseGraph(ctx context.Context, nodeId T) error {
	vertex, ok := r.dag.GetVertex(nodeId)
	if !ok {
		return fmt.Errorf("vertex for nodeId %v does not exist", nodeId)
	}
	r.vertices = append(r.vertices, vertex)

	// Get children and sort them for stable output
	children := graph.GetNeighborsSorted(ctx, vertex)

	for _, child := range children {
		if err := r.traverseGraph(ctx, child); err != nil {
			return err
		}
	}
	return nil
}

// renderObjects renders the vertices based on the specified output format.
func (r *Renderer[T]) renderObjects(writer io.Writer) error {
	if err := r.listSerializer.Serialize(writer, r.vertices); err != nil {
		return fmt.Errorf("failed to encode vertices: %w", err)
	}
	return nil
}
