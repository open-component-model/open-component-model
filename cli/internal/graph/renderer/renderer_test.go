package displaymanager

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/stretchr/testify/require"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
)

func TestTreeRendererLive(t *testing.T) {
	r := require.New(t)

	d := syncdag.NewDirectedAcyclicGraph[string]()
	r.NoError(d.AddVertex("A", map[string]any{syncdag.AttributeTraversalState: syncdag.StateDiscovering})) // Only root node initially

	buf := &bytes.Buffer{}
	logWriter := testLogWriter{t}
	writer := io.MultiWriter(buf, logWriter)
	vertexSerializer := func(v *syncdag.Vertex[string]) string {
		state, _ := v.Attributes.Load(syncdag.AttributeTraversalState)
		return v.ID + " (" + StateToString(state.(syncdag.TraversalState)) + ")"
	}

	renderer := NewTreeRenderer[string](d, vertexSerializer, WithMode(ModeLive), WithWriter(writer), WithRefreshRate(10*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	renderer.Start(ctx, "A")

	time.Sleep(30 * time.Millisecond)
	output := buf.String()
	expected := "── A (discovering)\n"
	r.Equal(expected, output)
	buf.Reset()

	// Add B as child of A
	r.NoError(d.AddVertex("B", map[string]any{syncdag.AttributeTraversalState: syncdag.StateDiscovering}))
	r.NoError(d.AddEdge("A", "B"))
	vB, _ := d.GetVertex("B")
	time.Sleep(30 * time.Millisecond)
	output = buf.String()
	expected = text.CursorUp.Sprint() + text.EraseLine.Sprint() + "── A (discovering)\n   ╰─ B (discovering)\n"
	r.Equal(expected, output)
	buf.Reset()

	// Add C as child of B
	r.NoError(d.AddVertex("C", map[string]any{syncdag.AttributeTraversalState: syncdag.StateDiscovering}))
	r.NoError(d.AddEdge("B", "C"))
	vC, _ := d.GetVertex("C")
	time.Sleep(30 * time.Millisecond)
	output = buf.String()
	expected = text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + "── A (discovering)\n   ╰─ B (discovering)\n      ╰─ C (discovering)\n"
	r.Equal(expected, output)
	buf.Reset()

	// Add D as another child of A
	r.NoError(d.AddVertex("D", map[string]any{syncdag.AttributeTraversalState: syncdag.StateDiscovering}))
	r.NoError(d.AddEdge("A", "D"))
	vD, _ := d.GetVertex("D")
	time.Sleep(30 * time.Millisecond)
	output = buf.String()
	expected = text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + "── A (discovering)\n   ├─ B (discovering)\n   │  ╰─ C (discovering)\n   ╰─ D (discovering)\n"
	r.Equal(expected, output)
	buf.Reset()

	// Mark D as completed
	vD.Attributes.Store(syncdag.AttributeTraversalState, syncdag.StateCompleted)
	time.Sleep(30 * time.Millisecond)
	output = buf.String()
	expected = text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + "── A (discovering)\n   ├─ B (discovering)\n   │  ╰─ C (discovering)\n   ╰─ D (completed)\n"
	r.Equal(expected, output)
	buf.Reset()

	// Mark C as completed
	vC.Attributes.Store(syncdag.AttributeTraversalState, syncdag.StateCompleted)
	time.Sleep(30 * time.Millisecond)
	output = buf.String()
	expected = text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + "── A (discovering)\n   ├─ B (discovering)\n   │  ╰─ C (completed)\n   ╰─ D (completed)\n"
	r.Equal(expected, output)
	buf.Reset()

	// Mark B as completed
	vB.Attributes.Store(syncdag.AttributeTraversalState, syncdag.StateCompleted)
	time.Sleep(30 * time.Millisecond)
	output = buf.String()
	expected = text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + "── A (discovering)\n   ├─ B (completed)\n   │  ╰─ C (completed)\n   ╰─ D (completed)\n"
	r.Equal(expected, output)
	buf.Reset()

	// Mark A as completed
	vA, _ := d.GetVertex("A")
	vA.Attributes.Store(syncdag.AttributeTraversalState, syncdag.StateCompleted)
	err := renderer.Wait(ctx)
	r.NoError(err)
	output = buf.String()
	expected = text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + "── A (completed)\n   ├─ B (completed)\n   │  ╰─ C (completed)\n   ╰─ D (completed)\n"
	r.Equal(expected, output)
}

func TestTreeRendererStatic(t *testing.T) {
	r := require.New(t)

	d := syncdag.NewDirectedAcyclicGraph[string]()
	r.NoError(d.AddVertex("A", map[string]any{syncdag.AttributeTraversalState: syncdag.StateDiscovering}))

	buf := &bytes.Buffer{}
	logWriter := testLogWriter{t}
	writer := io.MultiWriter(buf, logWriter)
	vertexSerializer := func(v *syncdag.Vertex[string]) string {
		state, _ := v.Attributes.Load(syncdag.AttributeTraversalState)
		return v.ID + " (" + StateToString(state.(syncdag.TraversalState)) + ")"
	}

	renderer := NewTreeRenderer[string](d, vertexSerializer, WithMode(ModeStatic), WithWriter(writer), WithRefreshRate(10*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	renderer.Start(ctx, "A")

	time.Sleep(30 * time.Millisecond)
	expected := "── A (discovering)\n"
	output := buf.String()
	r.Equal(expected, output)

	// Add B as child of A
	r.NoError(d.AddVertex("B", map[string]any{syncdag.AttributeTraversalState: syncdag.StateDiscovering}))
	r.NoError(d.AddEdge("A", "B"))
	vB, _ := d.GetVertex("B")
	time.Sleep(30 * time.Millisecond)
	output = buf.String()
	r.Equal(expected, output) // still only root

	// Add C as child of B
	r.NoError(d.AddVertex("C", map[string]any{syncdag.AttributeTraversalState: syncdag.StateDiscovering}))
	r.NoError(d.AddEdge("B", "C"))
	vC, _ := d.GetVertex("C")
	time.Sleep(30 * time.Millisecond)
	output = buf.String()
	r.Equal(expected, output) // still only root

	// Add D as another child of A
	r.NoError(d.AddVertex("D", map[string]any{syncdag.AttributeTraversalState: syncdag.StateDiscovering}))
	r.NoError(d.AddEdge("A", "D"))
	vD, _ := d.GetVertex("D")
	time.Sleep(30 * time.Millisecond)
	output = buf.String()
	r.Equal(expected, output) // still only root

	// Mark D as completed (still only root)
	vD.Attributes.Store(syncdag.AttributeTraversalState, syncdag.StateCompleted)
	time.Sleep(30 * time.Millisecond)
	output = buf.String()
	r.Equal(expected, output)

	// Mark C as completed (still only root)
	vC.Attributes.Store(syncdag.AttributeTraversalState, syncdag.StateCompleted)
	time.Sleep(30 * time.Millisecond)
	output = buf.String()
	r.Equal(expected, output)

	// Mark B as completed (still only root)
	vB.Attributes.Store(syncdag.AttributeTraversalState, syncdag.StateCompleted)
	time.Sleep(30 * time.Millisecond)
	expected += "   ├─ B (completed)\n   │  ╰─ C (completed)\n   ╰─ D (completed)\n"
	output = buf.String()
	r.Equal(expected, output)

	// Mark A as completed (now the full tree is appended)
	vA, _ := d.GetVertex("A")
	vA.Attributes.Store(syncdag.AttributeTraversalState, syncdag.StateCompleted)
	err := renderer.Wait(ctx)
	r.NoError(err)
	output = buf.String()
	r.Equal(expected, output)
}

type testLogWriter struct{ t *testing.T }

func (w testLogWriter) Write(p []byte) (int, error) {
	w.t.Log("\n" + string(p))
	return len(p), nil
}

func StateToString(state syncdag.TraversalState) string {
	switch state {
	case syncdag.StateDiscovering:
		return "discovering"
	case syncdag.StateDiscovered:
		return "discovered"
	case syncdag.StateCompleted:
		return "completed"
	case syncdag.StateError:
		return "error"
	default:
		return "unknown"
	}
}
