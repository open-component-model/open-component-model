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
	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/cli/internal/render"
)

func TestRunRenderLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)

		graph := syncdag.NewSyncedDirectedAcyclicGraph[string]()

		buf := &bytes.Buffer{}
		logWriter := testLogWriter{t}
		writer := io.MultiWriter(buf, logWriter)

		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)

		serializer := func(v *dag.Vertex[string]) (any, error) {
			state, ok := v.Attributes[syncdag.AttributeDiscoveryState]
			if !ok {
				return nil, fmt.Errorf("attribute %s not found for vertex %s", syncdag.AttributeDiscoveryState, v.ID)
			}
			discoveryState, ok := state.(syncdag.DiscoveryState)
			if !ok {
				return nil, fmt.Errorf("attribute %s for vertex %s is not of type %T", syncdag.AttributeDiscoveryState, v.ID, syncdag.DiscoveryState(0))
			}
			return map[string]any{
				"id":    v.ID,
				"state": discoveryState.String(),
			}, nil
		}
		renderer := New(ctx, graph, WithListSerializer(NewSerializer(WithVertexSerializerFunc(serializer), WithOutputFormat[string](render.OutputFormatJSON))))

		refreshRate := 10 * time.Millisecond
		waitFunc := render.RunRenderLoop(ctx, renderer, render.WithRefreshRate(refreshRate), render.WithRenderOptions(render.WithWriter(writer)))

		r.NoError(graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[string]) error {
			if err := d.AddVertex("A", map[string]any{syncdag.AttributeDiscoveryState: syncdag.DiscoveryStateDiscovering}); err != nil {
				return fmt.Errorf("failed to add vertex: %w", err)
			}
			return nil
		}))

		// sleep to allow ticker based render loop to start
		time.Sleep(refreshRate)
		// wait for the first render to complete
		// without this, the test would be flaky or fail
		synctest.Wait()
		output := buf.String()
		expected := `[
  {
    "id": "A",
    "state": "discovering"
  }
]
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
		r.NoError(graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[string]) error {
			if err := d.AddVertex("B", map[string]any{syncdag.AttributeDiscoveryState: syncdag.DiscoveryStateDiscovering}); err != nil {
				return fmt.Errorf("failed adding vertex: %w", err)
			}
			if err := d.AddEdge("A", "B"); err != nil {
				return fmt.Errorf("failed adding edge: %w", err)
			}
			return nil
		}))
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(6) + `[
  {
    "id": "A",
    "state": "discovering"
  },
  {
    "id": "B",
    "state": "discovering"
  }
]
`
		r.Equal(expected, output)
		buf.Reset()

		// Add C as child of B
		r.NoError(graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[string]) error {
			if err := d.AddVertex("C", map[string]any{syncdag.AttributeDiscoveryState: syncdag.DiscoveryStateDiscovering}); err != nil {
				return fmt.Errorf("failed adding vertex: %w", err)
			}
			if err := d.AddEdge("B", "C"); err != nil {
				return fmt.Errorf("failed adding edge: %w", err)
			}
			return nil
		}))
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(10) + `[
  {
    "id": "A",
    "state": "discovering"
  },
  {
    "id": "B",
    "state": "discovering"
  },
  {
    "id": "C",
    "state": "discovering"
  }
]
`
		r.Equal(expected, output)
		buf.Reset()

		// Add D as another child of A
		r.NoError(graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[string]) error {
			if err := d.AddVertex("D", map[string]any{syncdag.AttributeDiscoveryState: syncdag.DiscoveryStateDiscovering}); err != nil {
				return fmt.Errorf("failed adding vertex: %w", err)
			}
			if err := d.AddEdge("A", "D"); err != nil {
				return fmt.Errorf("failed adding edge: %w", err)
			}
			return nil
		}))
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(14) + `[
  {
    "id": "A",
    "state": "discovering"
  },
  {
    "id": "B",
    "state": "discovering"
  },
  {
    "id": "C",
    "state": "discovering"
  },
  {
    "id": "D",
    "state": "discovering"
  }
]
`
		r.Equal(expected, output)
		buf.Reset()

		// Mark D as completed
		r.NoError(graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[string]) error {
			d.Vertices["D"].Attributes[syncdag.AttributeDiscoveryState] = syncdag.DiscoveryStateCompleted
			return nil
		}))
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(18) + `[
  {
    "id": "A",
    "state": "discovering"
  },
  {
    "id": "B",
    "state": "discovering"
  },
  {
    "id": "C",
    "state": "discovering"
  },
  {
    "id": "D",
    "state": "completed"
  }
]
`
		r.Equal(expected, output)
		buf.Reset()

		// Mark C as completed
		r.NoError(graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[string]) error {
			d.Vertices["C"].Attributes[syncdag.AttributeDiscoveryState] = syncdag.DiscoveryStateCompleted
			return nil
		}))
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(18) + `[
  {
    "id": "A",
    "state": "discovering"
  },
  {
    "id": "B",
    "state": "discovering"
  },
  {
    "id": "C",
    "state": "completed"
  },
  {
    "id": "D",
    "state": "completed"
  }
]
`
		r.Equal(expected, output)
		buf.Reset()

		// Mark B as completed
		r.NoError(graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[string]) error {
			d.Vertices["B"].Attributes[syncdag.AttributeDiscoveryState] = syncdag.DiscoveryStateCompleted
			return nil
		}))
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(18) + `[
  {
    "id": "A",
    "state": "discovering"
  },
  {
    "id": "B",
    "state": "completed"
  },
  {
    "id": "C",
    "state": "completed"
  },
  {
    "id": "D",
    "state": "completed"
  }
]
`
		r.Equal(expected, output)
		buf.Reset()

		// Mark A as completed
		r.NoError(graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[string]) error {
			d.Vertices["A"].Attributes[syncdag.AttributeDiscoveryState] = syncdag.DiscoveryStateCompleted
			return nil
		}))
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(18) + `[
  {
    "id": "A",
    "state": "completed"
  },
  {
    "id": "B",
    "state": "completed"
  },
  {
    "id": "C",
    "state": "completed"
  },
  {
    "id": "D",
    "state": "completed"
  }
]
`
		r.Equal(expected, output)

		cancel()
		err := waitFunc()
		r.ErrorIs(err, context.Canceled)
	})
}

func TestRenderOnce(t *testing.T) {
	r := require.New(t)

	// We are cheating here. Since this logic is completely synchronous, we keep
	// using the reference to the raw dag and not the synced wrapper.
	// This makes adding vertices and edges much simpler.
	// DO NOT DO THIS IN PRODUCTION CODE!
	d := dag.NewDirectedAcyclicGraph[string]()
	graph := syncdag.ToSyncedGraph(d)

	buf := &bytes.Buffer{}
	logWriter := testLogWriter{t}
	writer := io.MultiWriter(buf, logWriter)

	ctx := t.Context()

	renderer := New(ctx, graph, WithListSerializer(NewSerializer(WithOutputFormat[string](render.OutputFormatJSON))))

	// Add A
	r.NoError(d.AddVertex("A"))
	expected := `[
  "A"
]
`
	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
	output := buf.String()
	buf.Reset()
	r.Equal(expected, output)

	// Add B
	r.NoError(d.AddVertex("B"))
	expected = `[
  "A",
  "B"
]
`
	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
	output = buf.String()
	buf.Reset()
	r.Equal(expected, output)

	// Add B as child of A
	r.NoError(d.AddEdge("A", "B"))
	expected = `[
  "A",
  "B"
]
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
	expected = `[
  "A",
  "B",
  "C",
  "D"
]
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
	//fmt.Print(string(p))
	w.t.Log("\n" + string(p))
	return len(p), nil
}
