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

type OutputFormat int

func (o OutputFormat) String() string {
	switch o {
	case OutputFormatJSON:
		return "json"
	case OutputFormatYAML:
		return "yaml"
	case OutputFormatNDJSON:
		return "ndjson"
	default:
		return fmt.Sprintf("unknown(%d)", o)
	}
}

const (
	AttributeComponentDescriptor = "component-descriptor"

	OutputFormatJSON OutputFormat = iota
	OutputFormatYAML
	OutputFormatNDJSON
)

type RendererOptions[T cmp.Ordered, U any] struct {
	// VertexMarshalizer converts a vertex to an object of type U. U is expected
	// to be a serializable type (e.g., a struct or map).
	// The marshalizer MUST perform READ-ONLY access to the vertex and its
	// attributes.
	VertexMarshalizer VertexMarshalizer[T, U]
}

type RendererOption[T cmp.Ordered, U any] func(*RendererOptions[T, U])

func WithVertexMarshalizer[T cmp.Ordered, U any](marshalizer VertexMarshalizer[T, U]) RendererOption[T, U] {
	return func(opts *RendererOptions[T, U]) {
		opts.VertexMarshalizer = marshalizer
	}
}

func WithVertexMarshalizerFunc[T cmp.Ordered, U any](marshalizerFunc func(*syncdag.Vertex[T]) U) RendererOption[T, U] {
	return func(opts *RendererOptions[T, U]) {
		opts.VertexMarshalizer = VertexMarshalizerFunc[T, U](marshalizerFunc)
	}
}

type VertexMarshalizer[T cmp.Ordered, U any] interface {
	Marshalize(*syncdag.Vertex[T]) U
}

type VertexMarshalizerFunc[T cmp.Ordered, U any] func(*syncdag.Vertex[T]) U

func (f VertexMarshalizerFunc[T, U]) Marshalize(v *syncdag.Vertex[T]) U {
	return f(v)
}

// Renderer renders a tree structure from a DirectedAcyclicGraph.
type Renderer[T cmp.Ordered, U any] struct {
	objects []U
	// VertexMarshalizer converts a vertex to an object of type U. U is expected
	// to be a serializable type (e.g., a struct or map).
	// The marshalizer MUST perform READ-ONLY access to the vertex and its
	// attributes.
	vertexMarshalizer VertexMarshalizer[T, U]
	outputFormat      OutputFormat
	root              T
	dag               *syncdag.DirectedAcyclicGraph[T]
}

// New creates a new Renderer for the given DirectedAcyclicGraph.
func New[T cmp.Ordered, U any](dag *syncdag.DirectedAcyclicGraph[T], root T, marshalizer VertexMarshalizer[T, U]) *Renderer[T, U] {
	return &Renderer[T, U]{
		objects:           make([]U, 0),
		vertexMarshalizer: marshalizer,
		root:              root,
		dag:               dag,
	}
}

// Render renders the tree structure starting from the root ID.
// It writes the output to the provided writer.
func (t *Renderer[T, U]) Render(ctx context.Context, writer io.Writer) error {
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

func (t *Renderer[T, U]) traverseGraph(ctx context.Context, nodeId T) error {
	vertex, ok := t.dag.GetVertex(nodeId)
	if !ok {
		return fmt.Errorf("vertex for nodeId %v does not exist", nodeId)
	}
	object := t.vertexMarshalizer.Marshalize(vertex)
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

func (t *Renderer[T, U]) renderObjects(writer io.Writer) error {
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

func (t *Renderer[T, U]) encodeObjectsAsNDJSON() ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	for _, obj := range t.objects {
		if err := encoder.Encode(obj); err != nil {
			return nil, fmt.Errorf("encoding component version descriptor failed: %w", err)
		}
	}
	return buf.Bytes(), nil
}

func (t *Renderer[T, U]) encodeObjectsAsJSON() ([]byte, error) {
	if len(t.objects) == 1 {
		return json.Marshal(t.objects[0])
	}

	return json.Marshal(t.objects)
}

func (t *Renderer[T, U]) encodeObjectsAsYAML() ([]byte, error) {
	if len(t.objects) == 1 {
		return yaml.Marshal(t.objects[0])
	}

	return yaml.Marshal(t.objects)
}
