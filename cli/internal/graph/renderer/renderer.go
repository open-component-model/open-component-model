package displaymanager

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/list"
	"github.com/jedib0t/go-pretty/v6/text"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
)

type GraphRenderer[T cmp.Ordered] interface {
	Render(ctx context.Context, writer io.Writer, dag *syncdag.DirectedAcyclicGraph[T], roots []T) error
}

func NewTreeRenderer[T cmp.Ordered](vertexSerializer func(*syncdag.Vertex[T]) string) *TreeRenderer[T] {
	if vertexSerializer == nil {
		vertexSerializer = func(v *syncdag.Vertex[T]) string {
			// Default serializer just returns the vertex ID.
			// This can be overridden by the user.
			return fmt.Sprintf("%v", v.ID)
		}
	}
	return &TreeRenderer[T]{
		listWriter:       list.NewWriter(),
		vertexSerializer: vertexSerializer,
	}
}

type TreeRenderer[T cmp.Ordered] struct {
	listWriter       list.Writer
	vertexSerializer func(*syncdag.Vertex[T]) string
}

func (t *TreeRenderer[T]) Render(ctx context.Context, writer io.Writer, dag *syncdag.DirectedAcyclicGraph[T], roots []T) error {
	t.listWriter.SetStyle(list.StyleConnectedRounded)
	defer t.listWriter.Reset()

	if len(roots) == 0 {
		return fmt.Errorf("no roots provided for rendering")
	} else if len(roots) > 1 {
		return fmt.Errorf("multiple roots provided for rendering, only one root is supported")
	}

	root := roots[0]
	_, exists := dag.GetVertex(root)
	if !exists {
		return fmt.Errorf("vertex for rootID %v does not exist", root)
	}

	if err := t.traverseGraph(ctx, dag, root); err != nil {
		return fmt.Errorf("failed to traverse graph: %w", err)
	}
	t.listWriter.SetOutputMirror(writer)
	t.listWriter.Render()
	return nil
}

func (t *TreeRenderer[T]) traverseGraph(ctx context.Context, dag *syncdag.DirectedAcyclicGraph[T], nodeId T) error {
	vertex, ok := dag.GetVertex(nodeId)
	if !ok {
		return fmt.Errorf("vertex for nodeId %v does not exist", nodeId)
	}
	item := t.vertexSerializer(vertex)
	t.listWriter.AppendItem(item)

	// Get children and sort them for stable output
	children := GetNeighborsSorted(vertex)

	for _, child := range children {
		t.listWriter.Indent()
		if err := t.traverseGraph(ctx, dag, child); err != nil {
			return err
		}
		t.listWriter.UnIndent()
	}
	return nil
}

// Render renders the current state of the tree starting from the given root ID.
// If no root is provided, it attempts to determine the root from the graph.
// If no writer is provided, it defaults to os.Stdout. If no GraphRenderer is
// provided, it uses a default tree renderer that serializes the vertex ID.
//
// Render is a convenience function around renderer.Render providing a similar
// interface as StartRenderLoop, but without the live rendering loop.
func Render[T cmp.Ordered](
	ctx context.Context,
	dag *syncdag.DirectedAcyclicGraph[T],
	roots []T,
	writer io.Writer,
	renderer GraphRenderer[T],
) error {
	if renderer == nil {
		renderer = NewTreeRenderer(func(v *syncdag.Vertex[T]) string {
			// Default serializer just returns the vertex ID.
			// This can be overridden by the user.
			return fmt.Sprintf("%v", v.ID)
		})
		slog.InfoContext(ctx, "no graph renderer provided, using default tree renderer")
	}

	if writer == nil {
		writer = os.Stdout
		slog.InfoContext(ctx, "no writer provided, using default os.Stdout for rendering")
	}

	if err := renderer.Render(ctx, writer, dag, roots); err != nil {
		return fmt.Errorf("failed to render graph: %w", err)
	}
	return nil
}

// StartRenderLoop starts the rendering loop. It returns a function that can be
// used to wait for the rendering loop to complete.
// The rendering loop will run until the vertex with root id is in
// syncdag.TraversalState StateCompleted, an error occurs or the context is
// canceled.
// If no root is provided, it attempts to determine the root from the graph.
// If no writer is provided, it defaults to os.Stdout. If no GraphRenderer is
// provided, it uses a default tree renderer that serializes the vertex ID.
// If no refresh rate is provided, it defaults to 100ms for live rendering.
func StartRenderLoop[T cmp.Ordered](
	ctx context.Context,
	dag *syncdag.DirectedAcyclicGraph[T],
	roots []T,
	writer io.Writer,
	refreshRate time.Duration,
	renderer GraphRenderer[T],
) func() error {
	errCh := make(chan error)
	waitFunc := func() error {
		select {
		case err := <-errCh:
			slog.InfoContext(ctx, "context canceled")
			return err
		}
	}

	if renderer == nil {
		renderer = NewTreeRenderer(func(v *syncdag.Vertex[T]) string {
			// Default serializer just returns the vertex ID.
			// This can be overridden by the user.
			return fmt.Sprintf("%v", v.ID)
		})
		slog.InfoContext(ctx, "no graph renderer provided, using default tree renderer")
	}

	if refreshRate == 0 {
		refreshRate = 100 * time.Millisecond
		slog.InfoContext(ctx, "no refresh rate provided, using default 100ms for live rendering")
	}

	if writer == nil {
		writer = os.Stdout
		slog.InfoContext(ctx, "no writer provided, using default os.Stdout for rendering")
	}

	renderState := &renderLoopState{
		errCh:       errCh,
		refreshRate: refreshRate,
		writer:      writer,
		outputState: struct {
			displayedLines int
			lastOutput     string
		}{
			displayedLines: 0,
			lastOutput:     "",
		},
	}

	go renderLoop(ctx, dag, roots, renderer, renderState)
	return waitFunc
}

type renderLoopState struct {
	errCh       chan error
	refreshRate time.Duration
	writer      io.Writer
	outputState struct {
		displayedLines int
		lastOutput     string
	}
}

func renderLoop[T cmp.Ordered](ctx context.Context, dag *syncdag.DirectedAcyclicGraph[T], roots []T, renderer GraphRenderer[T], renderState *renderLoopState) {
	ticker := time.NewTicker(renderState.refreshRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			renderState.errCh <- ctx.Err()
			close(renderState.errCh)
			return
		case <-ticker.C:
			if err := refreshOutput(ctx, dag, roots, renderer, renderState); err != nil {
				renderState.errCh <- err
				close(renderState.errCh)
				return
			}
		}
	}
}

func refreshOutput[T cmp.Ordered](ctx context.Context, dag *syncdag.DirectedAcyclicGraph[T], roots []T, renderer GraphRenderer[T], renderState *renderLoopState) error {
	outbuf := new(bytes.Buffer)
	if err := Render(ctx, dag, roots, outbuf, renderer); err != nil {
		return err
	}
	output := outbuf.String()

	// only update if the output has changed
	if !outputEquals(output, renderState.outputState.lastOutput) {
		// clear previous output
		var buf bytes.Buffer
		for range renderState.outputState.displayedLines {
			buf.WriteString(text.CursorUp.Sprint())
			buf.WriteString(text.EraseLine.Sprint())
		}
		if _, err := fmt.Fprint(renderState.writer, buf.String()); err != nil {
			return fmt.Errorf("error clearing previous output: %w", err)
		}

		if _, err := fmt.Fprint(renderState.writer, output); err != nil {
			return fmt.Errorf("error writing live rendering output to tree display manager writer: %w", err)
		}
		renderState.outputState.lastOutput = output
		renderState.outputState.displayedLines = strings.Count(output, "\n")
	}
	return nil
}

// GetNeighborsSorted returns the neighbors of the given vertex sorted by their
// order index if available, otherwise by their key.
// This function may be used to implement GraphRenderer with a consistent
// order of neighbors in the output.
func GetNeighborsSorted[T cmp.Ordered](vertex *syncdag.Vertex[T]) []T {
	type kv struct {
		Key   T
		Value *sync.Map
	}
	var kvSlice []kv

	vertex.Edges.Range(func(key, value any) bool {
		childId, ok1 := key.(T)
		attributes, ok2 := value.(*sync.Map)
		if ok1 && ok2 {
			kvSlice = append(kvSlice, kv{
				Key:   childId,
				Value: attributes,
			})
		}
		return true
	})

	// Sort kvSlice by order index if available, otherwise by key
	slices.SortFunc(kvSlice, func(a, b kv) int {
		var orderA, orderB int
		var okA, okB bool
		if oa, ok := a.Value.Load(syncdag.AttributeOrderIndex); ok {
			orderA, okA = oa.(int)
		}
		if ob, ok := b.Value.Load(syncdag.AttributeOrderIndex); ok {
			orderB, okB = ob.(int)
		}
		if okA && okB {
			return orderA - orderB
		} else if okA {
			return -1
		} else if okB {
			return 1
		}
		return cmp.Compare(a.Key, b.Key)
	})

	children := make([]T, len(kvSlice))
	for i, kv := range kvSlice {
		children[i] = kv.Key
	}
	return children
}

func outputEquals(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return a == b
}
