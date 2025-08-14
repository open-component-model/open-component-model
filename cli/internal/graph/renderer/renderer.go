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
	Render(ctx context.Context, writer io.Writer, dag *syncdag.DirectedAcyclicGraph[T], root T) error
}

func NewTreeRenderer[T cmp.Ordered](vertexSerializer func(*syncdag.Vertex[T]) string) *TreeRenderer[T] {
	return &TreeRenderer[T]{
		listWriter:       list.NewWriter(),
		vertexSerializer: vertexSerializer,
	}
}

type TreeRenderer[T cmp.Ordered] struct {
	listWriter       list.Writer
	vertexSerializer func(*syncdag.Vertex[T]) string
}

func (t *TreeRenderer[T]) Render(ctx context.Context, writer io.Writer, dag *syncdag.DirectedAcyclicGraph[T], root T) error {
	t.listWriter.SetStyle(list.StyleConnectedRounded)
	defer t.listWriter.Reset()

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
	children := getNeighborsSorted(vertex)

	for _, child := range children {
		t.listWriter.Indent()
		if err := t.traverseGraph(ctx, dag, child); err != nil {
			return err
		}
		t.listWriter.UnIndent()
	}
	return nil
}

type TreeRendererOption[T cmp.Ordered] func(*TreeRendererOptions[T])

type TreeRendererOptions[T cmp.Ordered] struct {
	GraphRenderer GraphRenderer[T]
	Writer        io.Writer
	RefreshRate   time.Duration
	Root          T
}

func WithGraphRenderer[T cmp.Ordered](renderer GraphRenderer[T]) TreeRendererOption[T] {
	return func(opts *TreeRendererOptions[T]) {
		opts.GraphRenderer = renderer
	}
}

func WithWriter[T cmp.Ordered](writer io.Writer) TreeRendererOption[T] {
	return func(opts *TreeRendererOptions[T]) {
		opts.Writer = writer
	}
}

func WithRoot[T cmp.Ordered](root T) TreeRendererOption[T] {
	return func(opts *TreeRendererOptions[T]) {
		opts.Root = root
	}
}

func WithRefreshRate[T cmp.Ordered](rate time.Duration) TreeRendererOption[T] {
	return func(opts *TreeRendererOptions[T]) {
		opts.RefreshRate = rate
	}
}

func WithTreeRendererOptions[T cmp.Ordered](opts *TreeRendererOptions[T]) TreeRendererOption[T] {
	return func(options *TreeRendererOptions[T]) {
		if opts.GraphRenderer != nil {
			options.GraphRenderer = opts.GraphRenderer
		}
		if opts.Writer != nil {
			options.Writer = opts.Writer
		}
		if opts.RefreshRate > 0 {
			options.RefreshRate = opts.RefreshRate
		}
		var zero T
		if opts.Root != zero {
			options.Root = opts.Root
		}
	}
}

func completeOptions[T cmp.Ordered](ctx context.Context, dag *syncdag.DirectedAcyclicGraph[T], opts []TreeRendererOption[T]) (*TreeRendererOptions[T], error) {
	options := &TreeRendererOptions[T]{}

	for _, opt := range opts {
		opt(options)
	}

	if options.GraphRenderer == nil {
		options.GraphRenderer = NewTreeRenderer(func(v *syncdag.Vertex[T]) string {
			// Default serializer just returns the vertex ID.
			// This can be overridden by the user.
			return fmt.Sprintf("%v", v.ID)
		})
	}

	if options.RefreshRate == 0 {
		options.RefreshRate = 100 * time.Millisecond
	}

	if options.Writer == nil {
		options.Writer = os.Stdout
		slog.InfoContext(ctx, "no writer provided, using default os.Stdout for rendering")
	}
	var zero T
	if options.Root == zero {
		roots := dag.Roots()
		if len(roots) > 1 {
			return options, fmt.Errorf("multiple roots found in the graph, please specify a root using WithRoot()")
		}
		if len(roots) == 0 {
			options.Root = zero
		} else {
			options.Root = roots[0]
		}
	}
	return options, nil
}

// Render renders the current state of the tree starting from the given root ID.
func Render[T cmp.Ordered](ctx context.Context, dag *syncdag.DirectedAcyclicGraph[T], opts ...TreeRendererOption[T]) error {
	options, err := completeOptions(ctx, dag, opts)
	if err != nil {
		return fmt.Errorf("failed to complete options: %w", err)
	}

	_, exists := dag.GetVertex(options.Root)
	if !exists {
		return fmt.Errorf("vertex for rootID %v does not exist", options.Root)
	}

	if err := options.GraphRenderer.Render(ctx, options.Writer, dag, options.Root); err != nil {
		return fmt.Errorf("failed to render graph: %w", err)
	}
	return nil
}

// StartRenderLoop starts the rendering loop. It returns a function that can be
// used to wait for the rendering loop to complete.
// The rendering loop will run until the vertex with root id is in
// syncdag.TraversalState StateCompleted, an error occurs or the context is
// canceled.
func StartRenderLoop[T cmp.Ordered](ctx context.Context, dag *syncdag.DirectedAcyclicGraph[T], opts ...TreeRendererOption[T]) func() error {
	doneCh := make(chan error)
	waitFunc := func() error {
		select {
		case err := <-doneCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	options, err := completeOptions(ctx, dag, opts)
	if err != nil {
		return func() error {
			return err
		}
	}

	renderState := &renderLoopState{
		doneCh:      doneCh,
		refreshRate: options.RefreshRate,
		outputState: struct {
			displayedLines int
			lastOutput     string
		}{
			displayedLines: 0,
			lastOutput:     "",
		},
	}

	go renderLoop(ctx, dag, options.Root, renderState, options)
	return waitFunc
}

type renderLoopState struct {
	doneCh      chan error
	refreshRate time.Duration
	outputState struct {
		displayedLines int
		lastOutput     string
	}
}

func renderLoop[T cmp.Ordered](ctx context.Context, dag *syncdag.DirectedAcyclicGraph[T], root T, renderState *renderLoopState, options *TreeRendererOptions[T]) {
	ticker := time.NewTicker(renderState.refreshRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			renderState.doneCh <- ctx.Err()
			return
		case <-ticker.C:
			if err, done := refreshOutput(ctx, dag, root, renderState, options); err != nil || done {
				renderState.doneCh <- err
				close(renderState.doneCh)
				return
			}
		}
	}
}

func refreshOutput[T cmp.Ordered](ctx context.Context, dag *syncdag.DirectedAcyclicGraph[T], root T, renderState *renderLoopState, options *TreeRendererOptions[T]) (error, bool) {
	vertex, ok := dag.GetVertex(root)
	if !ok {
		slog.InfoContext(ctx, "vertex for rootID does not exist yet, skipping rendering", "rootID", root)
		return nil, false
	}
	// It is important to retrieve the traversal state BEFORE rendering the graph.
	// Otherwise, there might be a race condition where the graph traversal
	// completes during the rendering process, but the vertex was not rendered
	// as completed yet.
	// Now, the worst case is that we do an additional unnecessary render loop
	// that does not affect the output.
	state, ok := vertex.Attributes.Load(syncdag.AttributeTraversalState)
	if !ok {
		return fmt.Errorf("vertex %v does not have a discovery state", root), true
	}

	outbuf := new(bytes.Buffer)
	if err := Render(ctx, dag, WithTreeRendererOptions(options), WithWriter[T](outbuf)); err != nil {
		return err, false
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
		if _, err := fmt.Fprint(options.Writer, buf.String()); err != nil {
			return fmt.Errorf("error clearing previous output: %w", err), false
		}

		if _, err := fmt.Fprint(options.Writer, output); err != nil {
			return fmt.Errorf("error writing live rendering output to tree display manager writer: %w", err), false
		}
		renderState.outputState.lastOutput = output
		renderState.outputState.displayedLines = strings.Count(output, "\n")
	}
	return nil, state == syncdag.StateCompleted
}

func getNeighborsSorted[T cmp.Ordered](vertex *syncdag.Vertex[T]) []T {
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
