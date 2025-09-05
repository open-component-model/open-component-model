package list

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

func TestRunRenderLoopYAML(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)

		d := syncdag.NewDirectedAcyclicGraph[string]()

		buf := &bytes.Buffer{}
		logWriter := testLogWriter{t}
		writer := io.MultiWriter(buf, logWriter)

		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)

		serializer := func(vertex *syncdag.Vertex[string]) (any, error) {
			state, ok := vertex.GetAttribute(syncdag.AttributeDiscoveryState)
			if !ok {
				return nil, fmt.Errorf("attribute %s not found for vertex %s", syncdag.AttributeDiscoveryState, vertex.ID)
			}
			DiscoveryState, ok := state.(syncdag.DiscoveryState)
			if !ok {
				return nil, fmt.Errorf("attribute %s for vertex %s is not of type %T", syncdag.AttributeDiscoveryState, vertex.ID, syncdag.DiscoveryState(0))
			}
			return map[string]any{
				"id":    vertex.ID,
				"state": DiscoveryState.String(),
			}, nil
		}
		renderer := New(ctx, d, WithListSerializer(NewSerializer(WithVertexSerializerFunc(serializer), WithOutputFormat[string](render.OutputFormatYAML))))

		refreshRate := 10 * time.Millisecond
		waitFunc := render.RunRenderLoop(ctx, renderer, render.WithRefreshRate(refreshRate), render.WithRenderOptions(render.WithWriter(writer)))

		r.NoError(d.AddVertex("A", map[string]any{syncdag.AttributeDiscoveryState: syncdag.DiscoveryStateDiscovering}))

		// sleep to allow ticker based render loop to start
		time.Sleep(refreshRate)
		// wait for the first render to complete
		// without this, the test would be flaky or fail
		synctest.Wait()
		output := buf.String()
		expected := `- id: A
  state: discovering
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
		expected = render.EraseNLines(2) + `- id: A
  state: discovering
- id: B
  state: discovering
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
		expected = render.EraseNLines(4) + `- id: A
  state: discovering
- id: B
  state: discovering
- id: C
  state: discovering
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
		expected = render.EraseNLines(6) + `- id: A
  state: discovering
- id: B
  state: discovering
- id: C
  state: discovering
- id: D
  state: discovering
`
		r.Equal(expected, output)
		buf.Reset()

		// Mark D as completed
		vD.Attributes.Store(syncdag.AttributeDiscoveryState, syncdag.DiscoveryStateCompleted)
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(8) + `- id: A
  state: discovering
- id: B
  state: discovering
- id: C
  state: discovering
- id: D
  state: completed
`
		r.Equal(expected, output)
		buf.Reset()

		// Mark C as completed
		vC.Attributes.Store(syncdag.AttributeDiscoveryState, syncdag.DiscoveryStateCompleted)
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(8) + `- id: A
  state: discovering
- id: B
  state: discovering
- id: C
  state: completed
- id: D
  state: completed
`
		r.Equal(expected, output)
		buf.Reset()

		// Mark B as completed
		vB.Attributes.Store(syncdag.AttributeDiscoveryState, syncdag.DiscoveryStateCompleted)
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(8) + `- id: A
  state: discovering
- id: B
  state: completed
- id: C
  state: completed
- id: D
  state: completed
`
		r.Equal(expected, output)
		buf.Reset()

		// Mark A as completed
		vA, _ := d.GetVertex("A")
		vA.Attributes.Store(syncdag.AttributeDiscoveryState, syncdag.DiscoveryStateCompleted)
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(8) + `- id: A
  state: completed
- id: B
  state: completed
- id: C
  state: completed
- id: D
  state: completed
`
		r.Equal(expected, output)

		cancel()
		err := waitFunc()
		r.ErrorIs(err, context.Canceled)
	})
}

func TestRenderOnceYAML(t *testing.T) {
	r := require.New(t)

	d := syncdag.NewDirectedAcyclicGraph[string]()

	buf := &bytes.Buffer{}
	logWriter := testLogWriter{t}
	writer := io.MultiWriter(buf, logWriter)

	ctx := t.Context()

	renderer := New(ctx, d, WithListSerializer(NewSerializer(WithOutputFormat[string](render.OutputFormatYAML))))

	// Add A
	r.NoError(d.AddVertex("A"))
	expected := `- A
`
	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
	output := buf.String()
	buf.Reset()
	r.Equal(expected, output)

	// Add B
	r.NoError(d.AddVertex("B"))
	expected = `- A
- B
`
	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
	output = buf.String()
	buf.Reset()
	r.Equal(expected, output)

	// Add B as child of A
	r.NoError(d.AddEdge("A", "B"))
	expected = `- A
- B
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
	expected = `- A
- B
- C
- D
`
	output = buf.String()
	buf.Reset()
	r.Equal(expected, output)

	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
	output = buf.String()
}
