package displaymanager

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/list"
	"github.com/jedib0t/go-pretty/v6/text"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
)

type TreeRendererOption func(*TreeRendererOptions)

type TreeRendererOptions struct {
	RefreshRate time.Duration
}

func WithRefreshRate(refreshRate time.Duration) TreeRendererOption {
	return func(opts *TreeRendererOptions) {
		opts.RefreshRate = refreshRate
	}
}

type RenderOption func(*RenderOptions)

type RenderOptions struct {
	Writer io.Writer
}

func WithWriter(writer io.Writer) RenderOption {
	return func(opts *RenderOptions) {
		opts.Writer = writer
	}
}

// TreeRenderer handles the rendering of a tree represented as part of a
// DirectedAcyclicGraph.
type TreeRenderer[T cmp.Ordered] struct {
	// The refreshRate is the time interval at which the rendering loop will
	// refresh the output during StartRenderLoop.
	// It defaults to 100 milliseconds if not set or set to 0.
	refreshRate time.Duration
	// The vertexSerializer is a function that serializes a vertex to a string.
	vertexSerializer func(v *syncdag.Vertex[T]) string
	// The dag is the DirectedAcyclicGraph that is to be displayed.
	// This is supposed to be a reference to the actual DirectedAcyclicGraph.
	// The rendering may run concurrently with the traversal. The concurrent
	// access to the dag is synchronized through sync.RWMutex and sync.Map.
	dag *syncdag.DirectedAcyclicGraph[T]
}

// NewTreeRenderer creates a new TreeRenderer for the given DirectedAcyclicGraph.
func NewTreeRenderer[T cmp.Ordered](dag *syncdag.DirectedAcyclicGraph[T], vertexSerializer func(v *syncdag.Vertex[T]) string, opts ...TreeRendererOption) *TreeRenderer[T] {
	options := &TreeRendererOptions{}

	for _, opt := range opts {
		opt(options)
	}

	if options.RefreshRate == 0 {
		options.RefreshRate = 100 * time.Millisecond
	}

	if vertexSerializer == nil {
		// Default serializer just returns the vertex ID.
		// The caller should override this to provide meaningful output.
		vertexSerializer = func(v *syncdag.Vertex[T]) string {
			return fmt.Sprintf("%v", v.ID)
		}
	}

	return &TreeRenderer[T]{
		refreshRate:      options.RefreshRate,
		vertexSerializer: vertexSerializer,
		dag:              dag,
	}
}

// StartRenderLoop starts the rendering loop. It returns a function that can be
// used to wait for the rendering loop to complete.
// The rendering loop will run until the vertex with root id is in
// syncdag.TraversalState StateCompleted, an error occurs or the context is
// canceled.
func (tdm *TreeRenderer[T]) StartRenderLoop(ctx context.Context, root T, opts ...RenderOption) func() error {
	options := &RenderOptions{}

	for _, opt := range opts {
		opt(options)
	}

	if options.Writer == nil {
		options.Writer = os.Stdout
	}

	doneCh := make(chan error)
	waitFunc := func() error {
		select {
		case err := <-doneCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	renderState := &renderLoopState{
		doneCh: doneCh,
		writer: options.Writer,
		outputState: struct {
			displayedLines int
			lastOutput     string
		}{
			displayedLines: 0,
			lastOutput:     "",
		},
		listWriter: list.NewWriter(),
	}

	go tdm.renderLoop(ctx, root, renderState)
	return waitFunc
}

type renderLoopState struct {
	doneCh      chan error
	writer      io.Writer
	outputState struct {
		displayedLines int
		lastOutput     string
	}
	listWriter list.Writer
}

func (tdm *TreeRenderer[T]) renderLoop(ctx context.Context, root T, renderState *renderLoopState) {
	ticker := time.NewTicker(tdm.refreshRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err, done := tdm.refreshOutput(ctx, root, renderState); err != nil || done {
				renderState.doneCh <- err
				close(renderState.doneCh)
				return
			}
		}
	}
}

func (tdm *TreeRenderer[T]) refreshOutput(_ context.Context, root T, renderState *renderLoopState) (error, bool) {
	vertex, ok := tdm.dag.GetVertex(root)
	if !ok {
		return fmt.Errorf("vertex for rootID %vertex does not exist", root), true
	}
	// It is important to retrieve the traversal state BEFORE rendering the graph.
	// Otherwise, there might be a race condition where the graph traversal
	// completes during the rendering process, but the vertex was not rendered
	// as completed yet.
	// Now, the worst case is that we do an additional unnecessary render loop
	// that does not affect the output.
	state, ok := vertex.Attributes.Load(syncdag.AttributeTraversalState)
	if !ok {
		return fmt.Errorf("vertex %vertex does not have a discovery state", root), true
	}

	renderState.listWriter.SetStyle(list.StyleConnectedRounded)
	defer renderState.listWriter.Reset()

	tdm.generateTreeOutput(root, renderState.listWriter)
	output := renderState.listWriter.Render()

	// only update if the output has changed
	if !tdm.outputEquals(output, renderState.outputState.lastOutput) {
		// clear previous output
		var buf bytes.Buffer
		for range renderState.outputState.displayedLines {
			buf.WriteString(text.CursorUp.Sprint())
			buf.WriteString(text.EraseLine.Sprint())
		}
		if _, err := fmt.Fprint(renderState.writer, buf.String()); err != nil {
			return fmt.Errorf("error clearing previous output: %w", err), false
		}

		if _, err := fmt.Fprintln(renderState.writer, output); err != nil {
			return fmt.Errorf("error writing live rendering output to tree display manager writer: %w", err), false
		}
		renderState.outputState.lastOutput = output
		renderState.outputState.displayedLines = renderState.listWriter.Length()
	}
	return nil, state == syncdag.StateCompleted
}

// Render renders the current state of the tree starting from the given root ID.
func (tdm *TreeRenderer[T]) Render(_ context.Context, root T, opts ...RenderOption) error {
	options := &RenderOptions{}
	for _, opt := range opts {
		opt(options)
	}
	if options.Writer == nil {
		options.Writer = os.Stdout
	}

	_, exists := tdm.dag.GetVertex(root)
	if !exists {
		return fmt.Errorf("vertex for rootID %v does not exist", root)
	}

	listWriter := list.NewWriter()
	listWriter.SetStyle(list.StyleConnectedRounded)
	defer listWriter.Reset()

	tdm.generateTreeOutput(root, listWriter)
	listWriter.SetOutputMirror(options.Writer)
	listWriter.Render()
	return nil
}

func (tdm *TreeRenderer[T]) outputEquals(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return a == b
}

func (tdm *TreeRenderer[T]) generateTreeOutput(nodeId T, listWriter list.Writer) {
	vertex, ok := tdm.dag.GetVertex(nodeId)
	if !ok {
		return
	}
	output := tdm.vertexSerializer(vertex)
	listWriter.AppendItem(output)

	// Get children and sort them for stable output
	children := tdm.getNeighborsSorted(vertex)

	for _, child := range children {
		listWriter.Indent()
		tdm.generateTreeOutput(child, listWriter)
		listWriter.UnIndent()
	}
}

func (tdm *TreeRenderer[T]) getNeighborsSorted(vertex *syncdag.Vertex[T]) []T {
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
