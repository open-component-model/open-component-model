package tree

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jedib0t/go-pretty/v6/table"

	"ocm.software/open-component-model/bindings/go/cli/internal/render/graph"
	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
)

// Renderer prints a tree from a DirectedAcyclicGraph as a table.
// Columns: NESTING, COMPONENT, VERSION, PROVIDER, IDENTITY.
// NESTING shows the tree structure using Unicode box-drawing characters from [TreeStyle].
//
// Example output:
//
//	NESTING   COMPONENT     VERSION  PROVIDER  IDENTITY
//	├─ ●      app-frontend  v1.2.0   acme      A
//	│  ├─ ●   ui-library    v2.1.0   acme      B
//	│  │  └─  icons         v1.0.0   other     C
//	│  └─     api-client    v3.0.0   acme      D
//	├─ ●      app-backend   v2.0.0   acme      X
//	│  └─     database      v1.1.0   acme      Y
//	└─        cache-layer   v0.9.0   other     Z
type Renderer[T cmp.Ordered] struct {
	// The tableWriter outputs a table and holds the visual NESTING column.
	// It manages the formatting/style and the output destination.
	tableWriter table.Writer
	// The VertexSerializer serializes a vertex to a Row struct.
	// It MUST perform READ-ONLY access to the vertex and its attributes.
	vertexSerializer VertexSerializer[T]
	// subRowProvider optionally yields additional leaf rows rendered as children
	// of a vertex (for example the resources of a component version).
	subRowProvider SubRowProvider[T]
	// Tree drawing style used for the NESTING column.
	style TreeStyle
	// Table style used for the go-pretty table renderer.
	tableStyle table.Style
	// The roots of the tree to render.
	// The roots are part of the Renderer instead of being passed to the
	// Render method to keep renderer.Renderer decoupled of specific data
	// structures.
	// The roots are optional. If not provided, the Renderer will
	// dynamically determine the roots from the DirectedAcyclicGraph.
	roots []T
	// The graph from which the tree is rendered.
	graph *syncdag.SyncedDirectedAcyclicGraph[T]
}

// New creates a new Renderer for the given DirectedAcyclicGraph.
func New[T cmp.Ordered](ctx context.Context, graph *syncdag.SyncedDirectedAcyclicGraph[T], opts ...RendererOption[T]) *Renderer[T] {
	options := &RendererOptions[T]{}
	for _, opt := range opts {
		opt(options)
	}

	if options.VertexSerializer == nil {
		options.VertexSerializer = VertexSerializerFunc[T](defaultVertexSerializer[T])
	}

	if len(options.Roots) == 0 {
		slog.DebugContext(ctx, "no roots provided, dynamically determining roots from graph")
	}

	return &Renderer[T]{
		tableWriter:      table.NewWriter(),
		vertexSerializer: options.VertexSerializer,
		subRowProvider:   options.SubRowProvider,
		style:            DefaultTreeStyle,
		tableStyle:       defaultTableStyle(),
		roots:            options.Roots,
		graph:            graph,
	}
}

// Render renders the tree structure starting from the root ID.
// It writes the output to the provided writer.
func (t *Renderer[T]) Render(ctx context.Context, writer io.Writer) error {
	defer t.tableWriter.ResetHeaders()
	defer t.tableWriter.ResetRows()
	t.tableWriter.SetStyle(t.tableStyle)
	t.tableWriter.AppendHeader(table.Row{"NESTING", "COMPONENT", "VERSION", "PROVIDER", "IDENTITY"})

	roots := t.roots
	if len(roots) == 0 {
		if err := t.graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[T]) error {
			roots = d.Roots()
			return nil
		}); err != nil {
			return fmt.Errorf("failed to auto-detect roots from graph: %w", err)
		}
		// We only do this for auto-detected roots. If the roots are provided,
		// we want to preserve the order.
		slices.Sort(roots)
	} else {
		filteredRoots := make([]T, 0, len(roots))
		if err := t.graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[T]) error {
			for _, root := range roots {
				if _, exists := d.Vertices[root]; exists {
					// If root does not exist in the graph yet, we exclude it from the
					// current rendering run.
					// The root might be added to the graph, after the rendering
					// has started, so we do not want to fail the rendering.
					filteredRoots = append(filteredRoots, root)
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("failed to filter non-existent roots: %w", err)
		}
		roots = filteredRoots
	}

	if err := t.graph.WithReadLock(func(lockedGraph *dag.DirectedAcyclicGraph[T]) error {
		slog.DebugContext(ctx, "locking graph for traversal", "roots", roots)
		defer func() {
			slog.DebugContext(ctx, "unlocking graph after traversal")
		}()

		for i, root := range roots {
			isLast := i == len(roots)-1
			if err := t.traverseGraph(ctx, lockedGraph, root, 0, true, isLast, nil); err != nil {
				return fmt.Errorf("failed to traverse graph: %w", err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	t.tableWriter.SetOutputMirror(writer)
	t.tableWriter.Render()
	return nil
}

// traverseGraph recursively walks the DAG and adds each vertex as a table row with proper tree nesting.
func (t *Renderer[T]) traverseGraph(ctx context.Context, lockedGraph *dag.DirectedAcyclicGraph[T], nodeId T, level int, isRoot, isLast bool, ancestorsHasMore []bool) error {
	vertex, ok := lockedGraph.Vertices[nodeId]
	if !ok {
		return fmt.Errorf("vertex for nodeId %v does not exist", nodeId)
	}
	row, err := t.vertexSerializer.Serialize(vertex)
	if err != nil {
		return fmt.Errorf("failed to serialize vertex %v: %w", vertex.ID, err)
	}

	// Determine children to build proper nesting tree
	children, err := graph.GetNeighborsSorted(ctx, vertex)
	if err != nil {
		return fmt.Errorf("failed to get sorted children of vertex %v: %w", vertex.ID, err)
	}

	// Optional leaf rows (for example resources) rendered as children of this
	// vertex, before the component-reference children.
	var subRows []Row
	if t.subRowProvider != nil {
		subRows, err = t.subRowProvider.SubRows(vertex)
		if err != nil {
			return fmt.Errorf("failed to build sub-rows of vertex %v: %w", vertex.ID, err)
		}
	}

	totalChildren := len(subRows) + len(children)
	hasChildren := totalChildren > 0
	nesting := buildNesting(t.style, ancestorsHasMore, isLast, hasChildren)
	t.tableWriter.AppendRow(table.Row{nesting, row.Component, row.Version, row.Provider, row.Identity})

	// The connector state passed down to all children of the current node.
	nextAncestors := make([]bool, 0, len(ancestorsHasMore)+1)
	nextAncestors = append(nextAncestors, ancestorsHasMore...)
	nextAncestors = append(nextAncestors, !isLast)

	childIndex := 0
	// Render sub-rows first as leaf children.
	for _, subRow := range subRows {
		childIsLast := childIndex == totalChildren-1
		subNesting := buildNesting(t.style, nextAncestors, childIsLast, false)
		t.tableWriter.AppendRow(table.Row{subNesting, subRow.Component, subRow.Version, subRow.Provider, subRow.Identity})
		childIndex++
	}

	// Recurse into component children.
	for _, child := range children {
		childIsLast := childIndex == totalChildren-1
		if err := t.traverseGraph(ctx, lockedGraph, child, level+1, false, childIsLast, nextAncestors); err != nil {
			return err
		}
		childIndex++
	}
	return nil
}

// buildNesting constructs the visual tree structure prefix for each row in the NESTING column.
// The function builds a string that represents the tree structure.
//
// Parameters:
//   - style: the tree drawing characters to use
//   - ancestorsHasMore: per-ancestor flags indicating whether that level has more siblings after the current node
//   - isLast: whether this is the last sibling at its level
//   - hasChildren: whether this node has child nodes
func buildNesting(style TreeStyle, ancestorsHasMore []bool, isLast bool, hasChildren bool) string {
	var result strings.Builder

	// Draw vertical connectors for each ancestor level
	// Each ancestor that has more siblings gets a vertical line [style.CharItemVertical], others get spaces
	verticalWidth := utf8.RuneCountInString(style.CharItemVertical)
	for _, hasMoreSiblings := range ancestorsHasMore {
		if hasMoreSiblings {
			result.WriteString(style.CharItemVertical)
		} else {
			result.WriteString(strings.Repeat(" ", verticalWidth))
		}
	}

	// Add the branch connector for this node
	// Use [style.CharItemMiddle] if there are more siblings after this one, [style.CharItemBottom] if this is the last
	if isLast {
		result.WriteString(style.CharItemBottom)
	} else {
		result.WriteString(style.CharItemMiddle)
	}

	// Add child indicator if this node has children
	if hasChildren {
		result.WriteString(style.CharChildIndicator)
	}

	return result.String()
}
