package displaymanager

import (
	"bytes"
	"context"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/stretchr/testify/require"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
)

func TestTreeRenderLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := require.New(t)

		d := syncdag.NewDirectedAcyclicGraph[string]()

		buf := &bytes.Buffer{}
		logWriter := testLogWriter{t}
		writer := io.MultiWriter(buf, logWriter)
		vertexSerializer := func(v *syncdag.Vertex[string]) string {
			state, _ := v.Attributes.Load(syncdag.AttributeTraversalState)
			return v.ID + " (" + string(state.(syncdag.TraversalState)) + ")"
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		r.NoError(d.AddVertex("A", map[string]any{syncdag.AttributeTraversalState: syncdag.StateDiscovering}))
		renderer := NewTreeRenderer[string](vertexSerializer)
		waitFunc := StartRenderLoop(ctx, d, WithWriter[string](writer), WithGraphRenderer[string](renderer), WithRefreshRate[string](10*time.Millisecond))

		time.Sleep(30 * time.Millisecond)
		//synctest.Wait()
		output := buf.String()
		expected := "── A (discovering)\n"
		r.Equal(expected, output)
		buf.Reset()

		// Add B as child of A
		r.NoError(d.AddVertex("B", map[string]any{syncdag.AttributeTraversalState: syncdag.StateDiscovering}))
		r.NoError(d.AddEdge("A", "B"))
		vB, _ := d.GetVertex("B")
		time.Sleep(30 * time.Millisecond)
		//synctest.Wait()
		output = buf.String()
		expected = text.CursorUp.Sprint() + text.EraseLine.Sprint() + "── A (discovering)\n   ╰─ B (discovering)\n"
		r.Equal(expected, output)
		buf.Reset()

		// Add C as child of B
		r.NoError(d.AddVertex("C", map[string]any{syncdag.AttributeTraversalState: syncdag.StateDiscovering}))
		r.NoError(d.AddEdge("B", "C"))
		vC, _ := d.GetVertex("C")
		time.Sleep(30 * time.Millisecond)
		//synctest.Wait()
		output = buf.String()
		expected = text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + "── A (discovering)\n   ╰─ B (discovering)\n      ╰─ C (discovering)\n"
		r.Equal(expected, output)
		buf.Reset()

		// Add D as another child of A
		r.NoError(d.AddVertex("D", map[string]any{syncdag.AttributeTraversalState: syncdag.StateDiscovering}))
		r.NoError(d.AddEdge("A", "D"))
		vD, _ := d.GetVertex("D")
		time.Sleep(30 * time.Millisecond)
		//synctest.Wait()
		output = buf.String()
		expected = text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + "── A (discovering)\n   ├─ B (discovering)\n   │  ╰─ C (discovering)\n   ╰─ D (discovering)\n"
		r.Equal(expected, output)
		buf.Reset()

		// Mark D as completed
		vD.Attributes.Store(syncdag.AttributeTraversalState, syncdag.StateCompleted)
		time.Sleep(30 * time.Millisecond)
		//synctest.Wait()
		output = buf.String()
		expected = text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + "── A (discovering)\n   ├─ B (discovering)\n   │  ╰─ C (discovering)\n   ╰─ D (completed)\n"
		r.Equal(expected, output)
		buf.Reset()

		// Mark C as completed
		vC.Attributes.Store(syncdag.AttributeTraversalState, syncdag.StateCompleted)
		time.Sleep(30 * time.Millisecond)
		//synctest.Wait()
		output = buf.String()
		expected = text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + "── A (discovering)\n   ├─ B (discovering)\n   │  ╰─ C (completed)\n   ╰─ D (completed)\n"
		r.Equal(expected, output)
		buf.Reset()

		// Mark B as completed
		vB.Attributes.Store(syncdag.AttributeTraversalState, syncdag.StateCompleted)
		time.Sleep(30 * time.Millisecond)
		//synctest.Wait()
		output = buf.String()
		expected = text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + "── A (discovering)\n   ├─ B (completed)\n   │  ╰─ C (completed)\n   ╰─ D (completed)\n"
		r.Equal(expected, output)
		buf.Reset()

		// Mark A as completed
		vA, _ := d.GetVertex("A")
		vA.Attributes.Store(syncdag.AttributeTraversalState, syncdag.StateCompleted)
		err := waitFunc()
		r.NoError(err)
		output = buf.String()
		expected = text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + text.CursorUp.Sprint() + text.EraseLine.Sprint() + "── A (completed)\n   ├─ B (completed)\n   │  ╰─ C (completed)\n   ╰─ D (completed)\n"
		r.Equal(expected, output)
	})
}

func TestTreeRendererStatic(t *testing.T) {
	r := require.New(t)

	d := syncdag.NewDirectedAcyclicGraph[string]()

	buf := &bytes.Buffer{}
	logWriter := testLogWriter{t}
	writer := io.MultiWriter(buf, logWriter)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r.NoError(d.AddVertex("A"))
	expected := "── A\n"
	r.NoError(Render[string](ctx, d, WithWriter[string](writer)))
	output := buf.String()
	buf.Reset()
	r.Equal(expected, output)

	// Add B
	r.NoError(d.AddVertex("B"))
	expected = "── A\n"
	r.NoError(Render[string](ctx, d, WithRoot("A"), WithWriter[string](writer)))
	output = buf.String()
	buf.Reset()
	r.Equal(expected, output)
	// Add B as child of A
	r.NoError(d.AddEdge("A", "B"))
	expected = "── A\n   ╰─ B\n"
	r.NoError(Render(ctx, d, WithWriter[string](writer)))
	output = buf.String()
	buf.Reset()
	r.Equal(expected, output) // still only root

	// Add C as child of B
	r.NoError(d.AddVertex("C"))
	r.NoError(d.AddEdge("B", "C"))

	// Add D as another child of A
	r.NoError(d.AddVertex("D"))
	r.NoError(d.AddEdge("A", "D"))

	r.NoError(Render[string](ctx, d, WithWriter[string](writer)))
	expected = "── A\n   ├─ B\n   │  ╰─ C\n   ╰─ D\n"
	output = buf.String()
	buf.Reset()
	r.Equal(expected, output)

	r.NoError(Render[string](ctx, d, WithWriter[string](writer)))
	output = buf.String()
}

type testLogWriter struct{ t *testing.T }

func (w testLogWriter) Write(p []byte) (int, error) {
	// This line can be commented in to see the actual output when running the
	// tests from a terminal supporting ANSI escape codes.
	//fmt.Print(string(p))
	w.t.Log("\n" + string(p))
	return len(p), nil
}
