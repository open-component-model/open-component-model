package list

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"

	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/cli/internal/renderer/graph"
	"sigs.k8s.io/yaml"
)

// Renderer renders a tree structure from a DirectedAcyclicGraph.
type Renderer[T cmp.Ordered] struct {
	objects []any
	// VertexMarshalizer converts a vertex to an object of type U. U is expected
	// to be a serializable type (e.g., a struct or map).
	// The marshalizer MUST perform READ-ONLY access to the vertex and its
	// attributes.
	vertexMarshalizer VertexMarshalizer[T]
	outputFormat      OutputFormat
	root              T
	dag               *syncdag.DirectedAcyclicGraph[T]
}

type VertexMarshalizer[T cmp.Ordered] interface {
	Marshalize(*syncdag.Vertex[T]) (any, error)
}

type VertexMarshalizerFunc[T cmp.Ordered] func(*syncdag.Vertex[T]) (any, error)

func (f VertexMarshalizerFunc[T]) Marshalize(v *syncdag.Vertex[T]) (any, error) {
	return f(v)
}

// New creates a new Renderer for the given DirectedAcyclicGraph.
func New[T cmp.Ordered](dag *syncdag.DirectedAcyclicGraph[T], root T, opts ...RendererOption[T]) *Renderer[T] {
	options := &RendererOptions[T]{}
	for _, opt := range opts {
		opt(options)
	}

	if options.VertexMarshalizer == nil {
		options.VertexMarshalizer = VertexMarshalizerFunc[T](func(v *syncdag.Vertex[T]) (any, error) {
			// Default marshalizer just returns the vertex ID.
			// This is supposed to be overridden by the user to provide a
			// meaningful representation.
			return fmt.Sprintf("%v", v.ID), nil
		})
	}

	if options.OutputFormat == 0 {
		options.OutputFormat = OutputFormatJSON
	}

	return &Renderer[T]{
		objects:           make([]any, 0),
		outputFormat:      options.OutputFormat,
		vertexMarshalizer: options.VertexMarshalizer,
		root:              root,
		dag:               dag,
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
	object, err := t.vertexMarshalizer.Marshalize(vertex)
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

func (t *Renderer[T]) renderObjects(writer io.Writer) error {
	var (
		err  error
		data []byte
	)
	switch t.outputFormat {
	case OutputFormatJSON:
		data, err = t.encodeObjectsAsJSON()
	case OutputFormatYAML:
		data, err = t.encodeObjectsAsYAML()
	case OutputFormatNDJSON:
		data, err = t.encodeObjectsAsNDJSON()
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

func (t *Renderer[T]) encodeObjectsAsNDJSON() ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	for _, obj := range t.objects {
		if err := encoder.Encode(obj); err != nil {
			return nil, fmt.Errorf("encoding component version descriptor failed: %w", err)
		}
	}
	return buf.Bytes(), nil
}

func (t *Renderer[T]) encodeObjectsAsJSON() ([]byte, error) {
	if len(t.objects) == 1 {
		return json.MarshalIndent(t.objects[0], "", "  ")
	}

	return json.MarshalIndent(t.objects, "", "  ")
}

func (t *Renderer[T]) encodeObjectsAsYAML() ([]byte, error) {
	if len(t.objects) == 1 {
		return yaml.Marshal(t.objects[0])
	}

	return yaml.Marshal(t.objects)
}
