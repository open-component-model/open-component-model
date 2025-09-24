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

	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/cli/internal/render/graph"
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
	// The dag from which the tree is rendered.
	dag *syncdag.DirectedAcyclicGraph[T]
}

// New creates a new Renderer for the given DirectedAcyclicGraph.
func New[T cmp.Ordered](ctx context.Context, dag *syncdag.DirectedAcyclicGraph[T], opts ...RendererOption[T]) *Renderer[T] {
	options := &RendererOptions[T]{}
	for _, opt := range opts {
		opt(options)
	}

	if options.VertexSerializer == nil {
		options.VertexSerializer = defaultVertexSerializer[T]()
	}

	if len(options.Roots) == 0 {
		slog.DebugContext(ctx, "no roots provided, dynamically determining roots from dag")
	}

	return &Renderer[T]{
		tableWriter:      table.NewWriter(),
		vertexSerializer: options.VertexSerializer,
		style:            DefaultTreeStyle,
		tableStyle:       defaultTableStyle(),
		roots:            options.Roots,
		dag:              dag,
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
		roots = t.dag.Roots()
		// We only do this for auto-detected roots. If the roots are provided,
		// we want to preserve the order.
		slices.Sort(roots)
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

	for i, root := range roots {
		isLast := i == len(roots)-1
		if err := t.traverseGraph(ctx, root, 0, true, isLast, nil); err != nil {
			return fmt.Errorf("failed to traverse graph: %w", err)
		}
	}
	t.tableWriter.SetOutputMirror(writer)
	t.tableWriter.Render()
	return nil
}

// traverseGraph recursively walks the DAG and adds each vertex as a table row with proper tree nesting.
func (t *Renderer[T]) traverseGraph(ctx context.Context, nodeId T, level int, isRoot, isLast bool, ancestorsHasMore []bool) error {
	vertex, ok := t.dag.GetVertex(nodeId)
	if !ok {
		return fmt.Errorf("vertex for nodeId %v does not exist", nodeId)
	}
	row, err := t.vertexSerializer.Serialize(vertex)
	if err != nil {
		return fmt.Errorf("failed to serialize vertex %v: %w", vertex.ID, err)
	}

	// Determine children to build proper nesting tree
	children := graph.GetNeighborsSorted(ctx, vertex)
	hasChildren := len(children) > 0
	nesting := buildNesting(t.style, ancestorsHasMore, isLast, hasChildren)
	t.tableWriter.AppendRow(table.Row{nesting, row.Component, row.Version, row.Provider, row.Identity})

	// Recurse into children
	for i, child := range children {
		childIsLast := i == len(children)-1
		// For descendants, include whether the current node has more siblings after it
		// Create a new slice to track ancestor vertical line states for the child nodes
		nextAncestors := make([]bool, 0, len(ancestorsHasMore)+1)
		// Copy all existing ancestor states (whether each ancestor level needs vertical lines)
		nextAncestors = append(nextAncestors, ancestorsHasMore...)
		// Determine if current node needs a vertical connector (true if it has siblings below it)
		connector := !isLast
		// Add current node's connector state to the ancestor tracking for its children
		nextAncestors = append(nextAncestors, connector)
		if err := t.traverseGraph(ctx, child, level+1, false, childIsLast, nextAncestors); err != nil {
			return err
		}
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
