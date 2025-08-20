package list

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"sigs.k8s.io/yaml"

	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/cli/internal/render"
	"ocm.software/open-component-model/cli/internal/render/graph"
)

// Renderer renders a tree from a DirectedAcyclicGraph as a flat last in a
// particular output format.
// The output rendered by the Renderer with OutputFormatJSON looks like this:
//
//	[
//	  "A",
//	  "B",
//	  "C",
//	  "D"
//	]
//
// The output is analogous to a tree structure, but without the indentation.
//
//	── A
//	   ├─ B
//	   │  ╰─ C
//	   ╰─ D
//
// Each letter corresponds to a vertex in the DirectedAcyclicGraph. The concrete
// representation of the vertex is defined by the VertexMarshaller.
type Renderer[T cmp.Ordered] struct {
	// The objects is a slice of objects that will be rendered.
	objects []any
	// The VertexMarshaller converts a vertex to an object that is added to objects.
	// The returned object is expected to be a serializable type (e.g., a struct
	// or map). The VertexMarshaller MUST perform READ-ONLY access to the vertex and its
	// attributes.
	vertexMarshaller VertexMarshaller[T]
	// The outputFormat specifies the format in which the output should be
	// rendered.
	outputFormat render.OutputFormat
	// The root ID of the tree to render.
	// The root ID is part of the Renderer instead of being passed to the
	// Render method to keep renderer.Renderer decoupled of specific data
	// structures.
	root T
	// The dag from which the tree is rendered.
	dag *syncdag.DirectedAcyclicGraph[T]
}

// VertexMarshaller is an interface that defines a method to create a
// serializable object from a vertex.
type VertexMarshaller[T cmp.Ordered] interface {
	Marshal(*syncdag.Vertex[T]) (any, error)
}

// VertexMarshallerFunc is a function type that implements the VertexMarshaller
// interface.
type VertexMarshallerFunc[T cmp.Ordered] func(*syncdag.Vertex[T]) (any, error)

// Marshal implements the VertexMarshaller interface for VertexMarshallerFunc.
func (f VertexMarshallerFunc[T]) Marshal(v *syncdag.Vertex[T]) (any, error) {
	return f(v)
}

// New creates a new Renderer for the given DirectedAcyclicGraph.
func New[T cmp.Ordered](dag *syncdag.DirectedAcyclicGraph[T], root T, opts ...RendererOption[T]) *Renderer[T] {
	options := &RendererOptions[T]{}
	for _, opt := range opts {
		opt(options)
	}

	if options.VertexMarshaller == nil {
		options.VertexMarshaller = VertexMarshallerFunc[T](func(v *syncdag.Vertex[T]) (any, error) {
			// Default marshaller just returns the vertex ID.
			// This is supposed to be overridden by the user to provide a
			// meaningful representation.
			return fmt.Sprintf("%v", v.ID), nil
		})
	}

	if options.OutputFormat == 0 {
		options.OutputFormat = render.OutputFormatJSON
	}

	return &Renderer[T]{
		objects:          make([]any, 0),
		outputFormat:     options.OutputFormat,
		vertexMarshaller: options.VertexMarshaller,
		root:             root,
		dag:              dag,
	}
}

// Render renders the tree structure starting from the root ID.
// It writes the output to the provided writer.
func (t *Renderer[T]) Render(ctx context.Context, writer io.Writer) error {
	defer func() {
		t.objects = t.objects[:0]
	}()
	var zero T
	if t.root == zero {
		return fmt.Errorf("root ID is not set")
	}

	_, exists := t.dag.GetVertex(t.root)
	if !exists {
		return fmt.Errorf("vertex for rootID %v does not exist", t.root)
	}

	if err := t.traverseGraph(ctx, t.root); err != nil {
		return fmt.Errorf("failed to traverse graph: %w", err)
	}
	if err := t.renderObjects(writer); err != nil {
		return err
	}

	return nil
}

func (t *Renderer[T]) traverseGraph(ctx context.Context, nodeId T) error {
	vertex, ok := t.dag.GetVertex(nodeId)
	if !ok {
		return fmt.Errorf("vertex for nodeId %v does not exist", nodeId)
	}
	object, err := t.vertexMarshaller.Marshal(vertex)
	if err != nil {
		return fmt.Errorf("failed to marshal vertex %v: %w", nodeId, err)
	}
	t.objects = append(t.objects, object)

	// Get children and sort them for stable output
	children := graph.GetNeighborsSorted(ctx, vertex)

	for _, child := range children {
		if err := t.traverseGraph(ctx, child); err != nil {
			return err
		}
	}
	return nil
}

// renderObjects renders the objects based on the specified output format.
func (t *Renderer[T]) renderObjects(writer io.Writer) error {
	var (
		err  error
		data []byte
	)
	switch t.outputFormat {
	case render.OutputFormatJSON:
		err = t.encodeObjectsAsJSON(writer)
	case render.OutputFormatYAML:
		err = t.encodeObjectsAsYAML(writer)
	case render.OutputFormatNDJSON:
		err = t.encodeObjectsAsNDJSON(writer)
	default:
		err = fmt.Errorf("unknown output format: %s", t.outputFormat.String())
	}
	if err != nil {
		return fmt.Errorf("failed to encode objects: %w", err)
	}
	if _, err := writer.Write(data); err != nil {
		return fmt.Errorf("failed to write encoded objects to writer: %w", err)
	}
	return err
}

func (t *Renderer[T]) encodeObjectsAsJSON(writer io.Writer) error {
	data, err := json.MarshalIndent(t.objects, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding multiple objects as JSON failed: %w", err)
	}

	// RunRenderLoop expects a newline at the end of the output.
	// Other formats - such as yaml - automatically add a newline at the end.
	data = append(data, '\n')

	if _, err = writer.Write(data); err != nil {
		return fmt.Errorf("failed to write JSON encoded objects to writer: %w", err)
	}
	return nil
}

func (t *Renderer[T]) encodeObjectsAsYAML(writer io.Writer) error {
	data, err := yaml.Marshal(t.objects)
	if err != nil {
		return fmt.Errorf("encoding objects as YAML failed: %w", err)
	}
	if _, err = writer.Write(data); err != nil {
		return fmt.Errorf("failed to write YAML encoded objects to writer: %w", err)
	}

	return nil
}

func (t *Renderer[T]) encodeObjectsAsNDJSON(writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	for _, obj := range t.objects {
		if err := encoder.Encode(obj); err != nil {
			return fmt.Errorf("encoding component version descriptor failed: %w", err)
		}
	}
	return nil
}
