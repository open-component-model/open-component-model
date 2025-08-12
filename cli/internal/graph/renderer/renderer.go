package displaymanager

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/list"
	"github.com/jedib0t/go-pretty/v6/text"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
)

const (
	// ModeLive uses terminal control sequences to clear already printed terminal
	// output and update the display dynamically.
	// WARNING: This mode is not supported on all terminals and may not work as
	// expected. In that case, use ModeStatic instead.
	ModeLive = "live"
	// ModeStatic uses a static display that does not update dynamically.
	// Tree branches are printed in order once they are fully discovered.
	ModeStatic = "static"
)

type TreeRendererOption func(*TreeRendererOptions)

type TreeRendererOptions struct {
	Mode        string
	RefreshRate time.Duration
	Writer      io.Writer
}

func WithMode(mode string) TreeRendererOption {
	return func(opts *TreeRendererOptions) {
		opts.Mode = mode
	}
}

func WithRefreshRate(refreshRate time.Duration) TreeRendererOption {
	return func(opts *TreeRendererOptions) {
		opts.RefreshRate = refreshRate
	}
}

func WithWriter(writer io.Writer) TreeRendererOption {
	return func(opts *TreeRendererOptions) {
		opts.Writer = writer
	}
}

// TreeRenderer handles the dynamic display of the component tree.
// The rendering logic is programmed against syncdag.TraversalState.
type TreeRenderer[T cmp.Ordered] struct {
	mu sync.RWMutex
	// done is a channel that signals when the display manager has finished.
	done chan error
	// mode is the display mode, either ModeLive or ModeStatic.
	mode string
	// refreshRate is the time interval at which the TreeRenderer fetches
	// and prints the state of the dag to writer.
	refreshRate time.Duration
	// writer is the io.Writer to which the output is written.
	// Typically, this is os.Stdout.
	writer io.Writer
	// listWriter is the writer used to render the tree structure.
	listWriter list.Writer
	// rootID is the ID of the root vertex of the tree to be displayed.
	rootID T
	// outputState keeps track of the current state of the output.
	// This is used to e.g. clear the previous output in ModeLive or to track
	// which lines have already been displayed in ModeStatic.
	outputState struct {
		skipLines      []int
		currentLine    int
		displayedLines int
		lastOutput     string
	}
	// vertexSerializer is a function that serializes a vertex to a string.
	// In case of mode ModeLive, the serialization of a vertex can include
	// discovery state information, e.g. "vertex name (discovering)" or
	// "vertex name (completed)".
	// In case of mode ModeStatic, the serialization of a vertex should be
	// idempotent for a vertex, independent of its discovery state, e.g.
	// "vertex name".
	vertexSerializer func(v *syncdag.Vertex[T]) string
	// dag is the DirectedAcyclicGraph that is to be displayed.
	// This is supposed to be a reference to the actual DirectedAcyclicGraph
	// being discovered. The concurrent access to the dag is synchronized
	// through sync.RWMutex and sync.Map.
	dag *syncdag.DirectedAcyclicGraph[T]
}

func NewTreeRenderer[T cmp.Ordered](dag *syncdag.DirectedAcyclicGraph[T], vertexSerializer func(v *syncdag.Vertex[T]) string, opts ...TreeRendererOption) *TreeRenderer[T] {
	options := &TreeRendererOptions{}

	for _, opt := range opts {
		opt(options)
	}

	if options.Mode == "" {
		options.Mode = ModeLive
	}
	if options.RefreshRate == 0 {
		options.RefreshRate = 100 * time.Millisecond
	}
	if options.Writer == nil {
		options.Writer = os.Stdout
	}
	if vertexSerializer == nil {
		// Default serializer just returns the vertex ID.
		// This should be overridden by the caller to provide meaningful output.
		vertexSerializer = func(v *syncdag.Vertex[T]) string {
			return fmt.Sprintf("%v", v.ID)
		}
	}

	listWriter := list.NewWriter()

	return &TreeRenderer[T]{
		done:             make(chan error),
		mode:             options.Mode,
		refreshRate:      options.RefreshRate,
		writer:           options.Writer,
		listWriter:       listWriter,
		vertexSerializer: vertexSerializer,
		dag:              dag,
		outputState: struct {
			skipLines      []int
			currentLine    int
			displayedLines int
			lastOutput     string
		}{currentLine: -1},
	}
}

func (tdm *TreeRenderer[T]) Start(ctx context.Context, root T) {
	tdm.mu.Lock()
	tdm.rootID = root
	tdm.mu.Unlock()

	go tdm.displayLoop(ctx)
}

func (tdm *TreeRenderer[T]) Wait(ctx context.Context) error {
	select {
	case err := <-tdm.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (tdm *TreeRenderer[T]) displayLoop(ctx context.Context) {
	ticker := time.NewTicker(tdm.refreshRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err, done := tdm.updateDisplay(); err != nil || done {
				tdm.done <- err
				close(tdm.done)
				return
			}
		}
	}
}

func (tdm *TreeRenderer[T]) updateDisplay() (error, bool) {
	tdm.mu.Lock()
	defer tdm.mu.Unlock()

	var zero T
	if tdm.rootID == zero {
		return nil, true
	}
	tdm.listWriter.SetStyle(list.StyleConnectedRounded)

	switch tdm.mode {
	case ModeLive:
		if err := tdm.renderLive(); err != nil {
			return fmt.Errorf("error rendering live display: %w", err), true
		}
	case ModeStatic:
		if err := tdm.renderStatic(); err != nil {
			return fmt.Errorf("error rendering static display: %w", err), true
		}
	}

	v, ok := tdm.dag.GetVertex(tdm.rootID)
	if !ok {
		return fmt.Errorf("vertex for rootID %v does not exist", tdm.rootID), true
	}
	state, ok := v.Attributes.Load(syncdag.AttributeTraversalState)
	if !ok {
		return fmt.Errorf("vertex %v does not have a discovery state", tdm.rootID), true
	}

	return nil, state == syncdag.StateCompleted
}

func (tdm *TreeRenderer[T]) renderLive() error {
	defer tdm.listWriter.Reset()

	tdm.generateTreeOutput(tdm.rootID)
	output := tdm.listWriter.Render()

	// only update if the output has changed
	if !tdm.outputEquals(output, tdm.outputState.lastOutput) {
		// clear previous output
		var buf bytes.Buffer
		for range tdm.outputState.displayedLines {
			buf.WriteString(text.CursorUp.Sprint())
			buf.WriteString(text.EraseLine.Sprint())
		}
		if _, err := fmt.Fprint(tdm.writer, buf.String()); err != nil {
			return fmt.Errorf("error clearing previous output: %w", err)
		}

		// write new output
		if _, err := fmt.Fprintln(tdm.writer, output); err != nil {
			return fmt.Errorf("error writing live rendering output to tree display manager writer: %w", err)
		}
		tdm.outputState.lastOutput = output
		tdm.outputState.displayedLines = tdm.listWriter.Length()
	}
	return nil
}

// TODO(fabianburth): static rendering could render the first children
//
//	immediately
func (tdm *TreeRenderer[T]) renderStatic() error {
	defer func() {
		tdm.listWriter.Reset()
		tdm.outputState.skipLines = []int{}
		tdm.outputState.currentLine = -1
	}()

	tdm.generateTreeOutput(tdm.rootID)
	//numberOfListItems := tdm.listWriter.Length()

	//rootVertex, ok := tdm.dag.GetVertex(tdm.rootID)
	//if !ok {
	//	return fmt.Errorf("vertex for rootID %v does not exist", tdm.rootID)
	//}
	//var err error
	//rootVertex.Edges.Range(func(key, _ any) bool {
	//	directNeighbor, ok := tdm.dag.GetVertex(key.(T))
	//	if !ok {
	//		err = fmt.Errorf("direct neighbor vertex %v for rootID %v does not exist", key, tdm.rootID)
	//		return false
	//	}
	//	state, ok := directNeighbor.Attributes.Load(syncdag.AttributeTraversalState)
	//	if !ok {
	//		err = fmt.Errorf("vertex %v does not have a discovery state", tdm.rootID)
	//	}
	//	switch state {
	//	case syncdag.StateCompleted:
	//		return true
	//	default:
	//		// In this case, there are still neighbored vertices to be discovered.
	//		// Since the list rendering renders to
	//		// -- rootID
	//		//    |-- child1
	//		//         |-- child1.1
	//		// while there is no other neighbor, but to
	//		// -- rootID
	//		//    |-- child1
	//		//    |    |-- child1.1\
	//		//    |-- child2
	//		// if there is another neighbor, the line of child1.1 would have to
	//		// change once the next neighbor is discovered.
	//		// Since this is not possible in case of static display, we add an empty
	//		// list item to the end of the list.
	//		// Since we only render the number of lines after generateTreeOutput(),
	//		// this additional empty item will not be displayed.
	//		tdm.listWriter.Indent()
	//		tdm.listWriter.AppendItem("")
	//		return false
	//	}
	//})
	//if err != nil {
	//	return fmt.Errorf("error checking discovery state of direct neighbors of rootID %v: %w", tdm.rootID, err)
	//}

	output := tdm.listWriter.Render()
	lines := strings.Split(output, "\n")
	outputLines := make([]string, 0)
	var lastOutputLines []string
	if tdm.outputState.lastOutput != "" {
		lastOutputLines = strings.Split(tdm.outputState.lastOutput, "\n")
	}
	for index, line := range lines {
		if slices.Contains(tdm.outputState.skipLines, index) {
			// Skip lines that are already displayed or that are marked to be skipped.
			continue
		}
		outputLines = append(outputLines, line)
	}
	if slices.Equal(lastOutputLines, outputLines) {
		// If the output has not changed, we do not need to update the display.
		return nil
	}
	//fmt.Printf("last output lines: %v / len %d, current output lines: %v\n", lastOutputLines, len(lastOutputLines), outputLines)
	//fmt.Printf("last output: %q, current output: %q\n", tdm.outputState.lastOutput, strings.Join(outputLines, "\n"))
	actualOutputLines := outputLines[len(lastOutputLines):]
	//actualOutput := strings.Join(actualOutputLines, "\n")

	for _, line := range actualOutputLines {
		_, err := fmt.Fprint(tdm.writer, line+"\n")
		if err != nil {
			return fmt.Errorf("error writing static rendering output to tree display manager writer: %w", err)
		}
	}
	tdm.outputState.lastOutput = strings.Join(outputLines, "\n")
	return nil

	//// Only update if the output has changed
	//if !tdm.outputEquals(output, tdm.outputState.lastOutput) {
	//	scanner := bufio.NewScanner(strings.NewReader(output))
	//	lineNum := 0
	//	actualOutput := &bytes.Buffer{}
	//	for scanner.Scan() {
	//		if lineNum < tdm.outputState.displayedLines || lineNum >= numberOfListItems || slices.Contains(tdm.outputState.skipLines, lineNum) {
	//			lineNum++
	//			continue
	//		}
	//		lineNum++
	//		_, err := fmt.Fprint(actualOutput, scanner.Text()+"\n")
	//		if err != nil {
	//			return fmt.Errorf("error writing to static rendering output buffer: %w", err)
	//		}
	//	}
	//	if err := scanner.Err(); err != nil {
	//		return fmt.Errorf("error reading list rendering output: %w", err)
	//	}
	//	_, err := fmt.Fprint(tdm.writer, actualOutput.String())
	//	if err != nil {
	//		return fmt.Errorf("error writing static rendering output to tree display manager writer: %w", err)
	//	}
	//
	//	tdm.outputState.lastOutput = output
	//	tdm.outputState.displayedLines = numberOfListItems
	//}
	//return nil
}

func (tdm *TreeRenderer[T]) outputEquals(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return a == b
}

func (tdm *TreeRenderer[T]) generateTreeOutput(nodeId T) {

	// Add current node
	vertex, ok := tdm.dag.GetVertex(nodeId)
	if !ok {
		//fmt.Fprintf(tdm.writer, "vertex for id %v does not exist\n", nodeId)
		return
	}
	output := tdm.vertexSerializer(vertex)
	tdm.listWriter.AppendItem(output)
	tdm.outputState.currentLine += 1

	// Get children and sort them for stable output
	children := tdm.getNeighborsSorted(vertex)

	for index, child := range children {
		vertex, _ := tdm.dag.GetVertex(child)
		switch tdm.mode {
		case ModeStatic:
			state, _ := vertex.Attributes.Load(syncdag.AttributeTraversalState)
			switch state {
			case syncdag.StateDiscovered:
				if index > 0 {
					prevvertex, _ := tdm.dag.GetVertex(children[index-1])
					state := prevvertex.MustGetAttribute(syncdag.AttributeTraversalState)
					if state != syncdag.StateCompleted {
						return
					}
				}
				tdm.listWriter.Indent()
				tdm.generateTreeOutput(child)
				if len(children) > index+1 {
					tdm.listWriter.AppendItem("")
					tdm.outputState.currentLine += 1
					tdm.outputState.skipLines = append(tdm.outputState.skipLines, tdm.outputState.currentLine)
				}
				tdm.listWriter.UnIndent()
				return
			case syncdag.StateCompleted:
				tdm.listWriter.Indent()
				tdm.generateTreeOutput(child)
				tdm.listWriter.UnIndent()
			default:
				return
			}
		case ModeLive:
			tdm.listWriter.Indent()
			tdm.generateTreeOutput(child)
			tdm.listWriter.UnIndent()
		}
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
