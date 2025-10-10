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
	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/cli/internal/render"
)

func withTestAttributes(state syncdag.DiscoveryState, name, version, provider string) map[string]any {
	return map[string]any{syncdag.AttributeDiscoveryState: state, syncdag.AttributeValue: &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    name,
					Version: version,
				},
			},
			Provider: descriptor.Provider{Name: provider},
		},
	}}
}

func TestRunRenderLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)

		graph := syncdag.NewSyncedDirectedAcyclicGraph[string]()

		buf := &bytes.Buffer{}
		logWriter := testLogWriter{t}
		writer := io.MultiWriter(buf, logWriter)
		vertexSerializer := func(vertex *dag.Vertex[string]) (Row, error) {
			untypedState, ok := vertex.Attributes[syncdag.AttributeDiscoveryState]
			if !ok {
				return Row{}, fmt.Errorf("vertex %v does not have a %s attribute", vertex.ID, syncdag.AttributeDiscoveryState)
			}
			state, ok := untypedState.(syncdag.DiscoveryState)
			if !ok {
				return Row{}, fmt.Errorf("vertex %v has a state attribute of unexpected type %T, expected type %T", vertex.ID, untypedState, syncdag.DiscoveryState(0))
			}
			untypedComponent, ok := vertex.Attributes[syncdag.AttributeValue]
			if !ok {
				return Row{}, fmt.Errorf("vertex %v does not have a %s attribute", vertex.ID, syncdag.AttributeValue)
			}
			component, ok := untypedComponent.(*descriptor.Descriptor)
			if !ok {
				return Row{}, fmt.Errorf("vertex %v has a value attribute of unexpected type %T, expected type %T", vertex.ID, untypedComponent, &descriptor.Descriptor{})
			}
			return Row{
				Component: fmt.Sprintf("%s (%s)", component.Component.Name, state),
				Version:   component.Component.Version,
				Provider:  component.Component.Provider.Name,
				Identity:  component.Component.ToIdentity().String(),
			}, nil
		}

		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		renderer := New[string](ctx, graph, WithVertexSerializerFunc(vertexSerializer))

		refreshRate := 10 * time.Millisecond
		waitFunc := render.RunRenderLoop(ctx, renderer, render.WithRefreshRate(refreshRate), render.WithRenderOptions(render.WithWriter(writer)))

		r.NoError(graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[string]) error {
			return d.AddVertex("A", withTestAttributes(syncdag.DiscoveryStateDiscovering, "comp-a", "v1.0.0", "acme"))
		}))

		// sleep to allow ticker based render loop to start
		time.Sleep(refreshRate)
		// wait for the first render to complete
		// without this, the test would be flaky or fail
		synctest.Wait()
		output := buf.String()
		expected := ` NESTING  COMPONENT             VERSION  PROVIDER  IDENTITY                   
 └─       comp-a (discovering)  v1.0.0   acme      name=comp-a,version=v1.0.0 
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
			if err := d.AddVertex("B", withTestAttributes(syncdag.DiscoveryStateDiscovering, "comp-b", "v2.0.0", "acme")); err != nil {
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
		expected = render.EraseNLines(2) + ` NESTING  COMPONENT             VERSION  PROVIDER  IDENTITY                   
 └─ ●     comp-a (discovering)  v1.0.0   acme      name=comp-a,version=v1.0.0 
    └─    comp-b (discovering)  v2.0.0   acme      name=comp-b,version=v2.0.0 
`
		r.Equal(expected, output)
		buf.Reset()

		// Add C as child of B
		r.NoError(graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[string]) error {
			if err := d.AddVertex("C", withTestAttributes(syncdag.DiscoveryStateDiscovering, "comp-c", "v1.5.0", "other")); err != nil {
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
		expected = render.EraseNLines(3) + ` NESTING   COMPONENT             VERSION  PROVIDER  IDENTITY                   
 └─ ●      comp-a (discovering)  v1.0.0   acme      name=comp-a,version=v1.0.0 
    └─ ●   comp-b (discovering)  v2.0.0   acme      name=comp-b,version=v2.0.0 
       └─  comp-c (discovering)  v1.5.0   other     name=comp-c,version=v1.5.0 
`
		r.Equal(expected, output)
		buf.Reset()

		r.NoError(graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[string]) error {
			if err := d.AddVertex("D", withTestAttributes(syncdag.DiscoveryStateDiscovering, "comp-d", "v3.0.0", "acme")); err != nil {
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
		expected = render.EraseNLines(4) + ` NESTING   COMPONENT             VERSION  PROVIDER  IDENTITY                   
 └─ ●      comp-a (discovering)  v1.0.0   acme      name=comp-a,version=v1.0.0 
    ├─ ●   comp-b (discovering)  v2.0.0   acme      name=comp-b,version=v2.0.0 
    │  └─  comp-c (discovering)  v1.5.0   other     name=comp-c,version=v1.5.0 
    └─     comp-d (discovering)  v3.0.0   acme      name=comp-d,version=v3.0.0 
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
		expected = render.EraseNLines(5) + ` NESTING   COMPONENT             VERSION  PROVIDER  IDENTITY                   
 └─ ●      comp-a (discovering)  v1.0.0   acme      name=comp-a,version=v1.0.0 
    ├─ ●   comp-b (discovering)  v2.0.0   acme      name=comp-b,version=v2.0.0 
    │  └─  comp-c (discovering)  v1.5.0   other     name=comp-c,version=v1.5.0 
    └─     comp-d (completed)    v3.0.0   acme      name=comp-d,version=v3.0.0 
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
		expected = render.EraseNLines(5) + ` NESTING   COMPONENT             VERSION  PROVIDER  IDENTITY                   
 └─ ●      comp-a (discovering)  v1.0.0   acme      name=comp-a,version=v1.0.0 
    ├─ ●   comp-b (discovering)  v2.0.0   acme      name=comp-b,version=v2.0.0 
    │  └─  comp-c (completed)    v1.5.0   other     name=comp-c,version=v1.5.0 
    └─     comp-d (completed)    v3.0.0   acme      name=comp-d,version=v3.0.0 
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
		expected = render.EraseNLines(5) + ` NESTING   COMPONENT             VERSION  PROVIDER  IDENTITY                   
 └─ ●      comp-a (discovering)  v1.0.0   acme      name=comp-a,version=v1.0.0 
    ├─ ●   comp-b (completed)    v2.0.0   acme      name=comp-b,version=v2.0.0 
    │  └─  comp-c (completed)    v1.5.0   other     name=comp-c,version=v1.5.0 
    └─     comp-d (completed)    v3.0.0   acme      name=comp-d,version=v3.0.0 
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
		expected = render.EraseNLines(5) + ` NESTING   COMPONENT           VERSION  PROVIDER  IDENTITY                   
 └─ ●      comp-a (completed)  v1.0.0   acme      name=comp-a,version=v1.0.0 
    ├─ ●   comp-b (completed)  v2.0.0   acme      name=comp-b,version=v2.0.0 
    │  └─  comp-c (completed)  v1.5.0   other     name=comp-c,version=v1.5.0 
    └─     comp-d (completed)  v3.0.0   acme      name=comp-d,version=v3.0.0 
`
		r.Equal(expected, output)
		buf.Reset()

		// Multiple roots
		r.NoError(graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[string]) error {
			if err := d.AddVertex("X", withTestAttributes(syncdag.DiscoveryStateDiscovering, "comp-d", "v3.0.0", "acme")); err != nil {
				return err
			}
			if err := d.AddVertex("Y", withTestAttributes(syncdag.DiscoveryStateDiscovering, "comp-d", "v3.0.0", "acme")); err != nil {
				return err
			}
			if err := d.AddVertex("Z", withTestAttributes(syncdag.DiscoveryStateDiscovering, "comp-d", "v3.0.0", "acme")); err != nil {
				return err
			}
			if err := d.AddEdge("X", "Z"); err != nil {
				return err
			}
			return nil
		}))
		time.Sleep(refreshRate)
		synctest.Wait()
		output = buf.String()
		expected = render.EraseNLines(5) + ` NESTING   COMPONENT             VERSION  PROVIDER  IDENTITY                   
 ├─ ●      comp-a (completed)    v1.0.0   acme      name=comp-a,version=v1.0.0 
 │  ├─ ●   comp-b (completed)    v2.0.0   acme      name=comp-b,version=v2.0.0 
 │  │  └─  comp-c (completed)    v1.5.0   other     name=comp-c,version=v1.5.0 
 │  └─     comp-d (completed)    v3.0.0   acme      name=comp-d,version=v3.0.0 
 ├─ ●      comp-d (discovering)  v3.0.0   acme      name=comp-d,version=v3.0.0 
 │  └─     comp-d (discovering)  v3.0.0   acme      name=comp-d,version=v3.0.0 
 └─        comp-d (discovering)  v3.0.0   acme      name=comp-d,version=v3.0.0 
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

	// We are cheating here. Since this logic is completely synchronous, we keep
	// using the reference to the raw dag and not the synced wrapper.
	// This makes adding vertices and edges much simpler.
	// DO NOT DO THIS IN PRODUCTION CODE!
	d := dag.NewDirectedAcyclicGraph[string]()
	graph := syncdag.ToSyncedGraph(d)

	buf := &bytes.Buffer{}
	logWriter := testLogWriter{t}
	writer := io.MultiWriter(buf, logWriter)

	renderer := New(ctx, graph)

	r.NoError(d.AddVertex("A", withTestAttributes(syncdag.DiscoveryStateDiscovering, "comp-a", "v1.0.0", "acme")))
	expected := ` NESTING  COMPONENT  VERSION  PROVIDER  IDENTITY                   
 └─       comp-a     v1.0.0   acme      name=comp-a,version=v1.0.0 
`
	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
	output := buf.String()
	buf.Reset()
	r.Equal(expected, output)

	// Add B
	r.NoError(d.AddVertex("B", withTestAttributes(syncdag.DiscoveryStateDiscovering, "comp-b", "v2.0.0", "acme")))
	expected = ` NESTING  COMPONENT  VERSION  PROVIDER  IDENTITY                   
 ├─       comp-a     v1.0.0   acme      name=comp-a,version=v1.0.0 
 └─       comp-b     v2.0.0   acme      name=comp-b,version=v2.0.0 
`
	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
	output = buf.String()
	buf.Reset()
	r.Equal(expected, output)
	// Add B as child of A
	r.NoError(d.AddEdge("A", "B"))
	expected = ` NESTING  COMPONENT  VERSION  PROVIDER  IDENTITY                   
 └─ ●     comp-a     v1.0.0   acme      name=comp-a,version=v1.0.0 
    └─    comp-b     v2.0.0   acme      name=comp-b,version=v2.0.0 
`
	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
	output = buf.String()
	buf.Reset()
	r.Equal(expected, output)

	// Add C as child of B
	r.NoError(d.AddVertex("C", withTestAttributes(syncdag.DiscoveryStateDiscovering, "comp-c", "v1.5.0", "other")))
	r.NoError(d.AddEdge("B", "C"))

	// Add D as another child of A
	r.NoError(d.AddVertex("D", withTestAttributes(syncdag.DiscoveryStateDiscovering, "comp-d", "v3.0.0", "acme")))
	r.NoError(d.AddEdge("A", "D"))

	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
	expected = ` NESTING   COMPONENT  VERSION  PROVIDER  IDENTITY                   
 └─ ●      comp-a     v1.0.0   acme      name=comp-a,version=v1.0.0 
    ├─ ●   comp-b     v2.0.0   acme      name=comp-b,version=v2.0.0 
    │  └─  comp-c     v1.5.0   other     name=comp-c,version=v1.5.0 
    └─     comp-d     v3.0.0   acme      name=comp-d,version=v3.0.0 
`
	output = buf.String()
	buf.Reset()
	r.Equal(expected, output)

	r.NoError(render.RenderOnce(ctx, renderer, render.WithWriter(writer)))
}

type testLogWriter struct{ t *testing.T }

func (w testLogWriter) Write(p []byte) (int, error) {
	// This line can be commented in to see the actual output when running the
	// tests from a terminal supporting ANSI escape codes.
	// fmt.Print(string(p))
	w.t.Log("\n" + string(p))
	return len(p), nil
}
