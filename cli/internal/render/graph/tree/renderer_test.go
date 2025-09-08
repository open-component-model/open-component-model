package tree

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/cli/internal/render"
)

func TestRunRenderLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)

		d := syncdag.NewDirectedAcyclicGraph[string]()

		buf := &bytes.Buffer{}
		logWriter := testLogWriter{t}
		writer := io.MultiWriter(buf, logWriter)
		vertexSerializer := func(v *syncdag.Vertex[string]) (string, error) {
			state, _ := v.Attributes.Load(syncdag.AttributeDiscoveryState)
			return fmt.Sprintf("%s (%s)", v.ID, state.(syncdag.DiscoveryState)), nil
		}

		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		renderer := New[string](ctx, d, WithVertexSerializerFunc(vertexSerializer))

		refreshRate := 10 * time.Millisecond
		waitFunc := render.RunRenderLoop(ctx, renderer, render.WithRefreshRate(refreshRate), render.WithRenderOptions(render.WithWriter(writer)))

		r.NoError(d.AddVertex("A", map[string]any{syncdag.AttributeDiscoveryState: syncdag.DiscoveryStateDiscovering}))

		// sleep to allow ticker based render loop to start
		time.Sleep(refreshRate)
		// wait for the first render to complete
		// without this, the test would be flaky or fail
		synctest.Wait()
		output := buf.String()
		expected := `── A (discovering)
`
		r.Equal(expected, output)
		buf.Reset()

		// Check that render loop does not print the output if it is equal to
		// the last output.

		// allow at least one more render loop to start
		time.Sleep(refreshRate)
		// again, wait for the render loop to complete
		synctest.Wait()
		output = buf.String()
		expected = ""
		r.Equal(expected, output)
		buf.Reset()

		// Add B as child of A
		r.NoError(d.AddVertex("B", map[string]any{syncdag.AttributeDiscoveryState: syncdag.DiscoveryStateDiscovering}))
		r.NoError(d.AddEdge("A", "B"))
		vB, _ := d.GetVertex("B")
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(1) + `── A (discovering)
   ╰─ B (discovering)
`
		r.Equal(expected, output)
		buf.Reset()

		// Add C as child of B
		r.NoError(d.AddVertex("C", map[string]any{syncdag.AttributeDiscoveryState: syncdag.DiscoveryStateDiscovering}))
		r.NoError(d.AddEdge("B", "C"))
		vC, _ := d.GetVertex("C")
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(2) + `── A (discovering)
   ╰─ B (discovering)
      ╰─ C (discovering)
`
		r.Equal(expected, output)
		buf.Reset()

		// Add D as another child of A
		r.NoError(d.AddVertex("D", map[string]any{syncdag.AttributeDiscoveryState: syncdag.DiscoveryStateDiscovering}))
		r.NoError(d.AddEdge("A", "D"))
		vD, _ := d.GetVertex("D")
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(3) + `── A (discovering)
   ├─ B (discovering)
   │  ╰─ C (discovering)
   ╰─ D (discovering)
`
		r.Equal(expected, output)
		buf.Reset()

		// Mark D as completed
		vD.Attributes.Store(syncdag.AttributeDiscoveryState, syncdag.DiscoveryStateCompleted)
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(4) + `── A (discovering)
   ├─ B (discovering)
   │  ╰─ C (discovering)
   ╰─ D (completed)
`
		r.Equal(expected, output)
		buf.Reset()

		// Mark C as completed
		vC.Attributes.Store(syncdag.AttributeDiscoveryState, syncdag.DiscoveryStateCompleted)
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(4) + `── A (discovering)
   ├─ B (discovering)
   │  ╰─ C (completed)
   ╰─ D (completed)
`
		r.Equal(expected, output)
		buf.Reset()

		// Mark B as completed
		vB.Attributes.Store(syncdag.AttributeDiscoveryState, syncdag.DiscoveryStateCompleted)
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(4) + `── A (discovering)
   ├─ B (completed)
   │  ╰─ C (completed)
   ╰─ D (completed)
`
		r.Equal(expected, output)
		buf.Reset()

		// Mark A as completed
		vA, _ := d.GetVertex("A")
		vA.Attributes.Store(syncdag.AttributeDiscoveryState, syncdag.DiscoveryStateCompleted)
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(4) + `── A (completed)
   ├─ B (completed)
   │  ╰─ C (completed)
   ╰─ D (completed)
`
		r.Equal(expected, output)

		cancel()
		err := waitFunc()
		r.ErrorIs(err, context.Canceled)
	})
}

func TestRenderOnce(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	d := syncdag.NewDirectedAcyclicGraph[string]()

	buf := &bytes.Buffer{}
	logWriter := testLogWriter{t}
	writer := io.MultiWriter(buf, logWriter)

	renderer := New(ctx, d)

	r.NoError(d.AddVertex("A"))
	expected := `── A
`
	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
	output := buf.String()
	buf.Reset()
	r.Equal(expected, output)

	// Add B
	r.NoError(d.AddVertex("B"))
	expected = `╭─ A
╰─ B
`
	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
	output = buf.String()
	buf.Reset()
	r.Equal(expected, output)
	// Add B as child of A
	r.NoError(d.AddEdge("A", "B"))
	expected = `── A
   ╰─ B
`
	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
	output = buf.String()
	buf.Reset()
	r.Equal(expected, output)

	// Add C as child of B
	r.NoError(d.AddVertex("C"))
	r.NoError(d.AddEdge("B", "C"))

	// Add D as another child of A
	r.NoError(d.AddVertex("D"))
	r.NoError(d.AddEdge("A", "D"))

	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
	expected = `── A
   ├─ B
   │  ╰─ C
   ╰─ D
`
	output = buf.String()
	buf.Reset()
	r.Equal(expected, output)

	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
	output = buf.String()
}

type testLogWriter struct{ t *testing.T }

func (w testLogWriter) Write(p []byte) (int, error) {
	// This line can be commented in to see the actual output when running the
	// tests from a terminal supporting ANSI escape codes.
	// fmt.Print(string(p))
	w.t.Log("\n" + string(p))
	return len(p), nil
}
