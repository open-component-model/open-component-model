package jsontree

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"

	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/renderer/graph"
	"sigs.k8s.io/yaml"
)

const (
	AttributeComponentDescriptor = "componentDescriptor"
)

//type OutputFormat int
//
//func (o OutputFormat) String() string {
//	switch o {
//	case OutputFormatJSON:
//		return "json"
//	case OutputFormatYAML:
//		return "yaml"
//	default:
//		return fmt.Sprintf("unknown(%d)", o)
//	}
//}
//
//const (
//	OutputFormatJSON OutputFormat = iota
//	OutputFormatYAML
//)

//type TreeRendererOptions[T cmp.Ordered] struct {
//	// VertexSerializer is a function that serializes a vertex to a string.
//	VertexSerializer func(*syncdag.Vertex[T]) string
//}
//
//type TreeRendererOption[T cmp.Ordered] func(*TreeRendererOptions[T])
//
//func WithVertexSerializer[T cmp.Ordered](serializer func(*syncdag.Vertex[T]) string) TreeRendererOption[T] {
//	return func(opts *TreeRendererOptions[T]) {
//		opts.VertexSerializer = serializer
//	}
//}

// Renderer renders a tree structure from a DirectedAcyclicGraph.
type Renderer[T cmp.Ordered, U any] struct {
	objects []U
	encoder func(U) ([]byte, error)
	root    T
	dag     *syncdag.DirectedAcyclicGraph[T]
}

//// NewTreeRenderer creates a new TreeRenderer for the given DirectedAcyclicGraph.
//func NewTreeRenderer[T cmp.Ordered](dag *syncdag.DirectedAcyclicGraph[T], root T, opts ...TreeRendererOption[T]) *TreeRenderer[T] {
//	options := &TreeRendererOptions[T]{}
//	for _, opt := range opts {
//		opt(options)
//	}
//
//	if options.VertexSerializer == nil {
//		options.VertexSerializer = func(v *syncdag.Vertex[T]) string {
//			// Default serializer just returns the vertex ID.
//			// This is supposed to be overridden by the user to provide a
//			// meaningful representation.
//			return fmt.Sprintf("%v", v.ID)
//		}
//	}
//	return &TreeRenderer[T]{
//		listWriter:       list.NewWriter(),
//		vertexSerializer: options.VertexSerializer,
//		root:             root,
//		dag:              dag,
//	}
//}

// Render renders the tree structure starting from the root ID.
// It writes the output to the provided writer.
func (t *Renderer[T, U]) Render(ctx context.Context, writer io.Writer) error {
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
	r, _, err := encodeDescriptors(t.outputFormat, t.componentDescriptors)
	if err != nil {
		return fmt.Errorf("failed to encode descriptors: %w", err)
	}

	buf, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("failed to read encoded descriptors: %w", err)
	}
	_, err = writer.Write(buf)
	if err != nil {
		return fmt.Errorf("failed to write encoded descriptors to writer: %w", err)
	}
	return nil
}

func (t *Renderer[T]) traverseGraph(ctx context.Context, nodeId T) error {
	vertex, ok := t.dag.GetVertex(nodeId)
	if !ok {
		return fmt.Errorf("vertex for nodeId %v does not exist", nodeId)
	}
	descriptor, ok := vertex.GetAttribute(AttributeComponentDescriptor)
	if !ok {
		return fmt.Errorf("failed to get attribute %s from vertex %s", AttributeComponentDescriptor, nodeId)
	}
	desc, ok := descriptor.(*descruntime.Descriptor)
	if !ok {
		return fmt.Errorf("expected attribute %s to be of type %T, got %T", AttributeComponentDescriptor, &descruntime.Descriptor{}, descriptor)
	}
	t.componentDescriptors = append(t.componentDescriptors, desc)

	// Get children and sort them for stable output
	children := graph.GetNeighborsSorted(ctx, vertex)

	for _, child := range children {
		if err := t.traverseGraph(ctx, child); err != nil {
			return err
		}
	}
	return nil
}

func encodeDescriptors(output string, descs []*descruntime.Descriptor) (io.Reader, int64, error) {
	var data []byte
	var err error
	switch output {
	case "json":
		data, err = encodeDescriptorsAsNDJSON(descs)
	case "yaml":
		data, err = encodeDescriptorsAsYAML(descs)
	default:
		err = fmt.Errorf("unknown output format: %q", output)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("encoding component version descriptor as %q failed: %w", output, err)
	}
	return bytes.NewReader(data), int64(len(data)), nil
}

func encodeDescriptorsAsNDJSON(descs []*descruntime.Descriptor) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	for _, desc := range descs {
		// TODO(jakobmoellerdev): add formatting options for scheme version with v2 as only option
		v2descriptor, err := descruntime.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
		if err != nil {
			return nil, fmt.Errorf("converting component version to v2 descriptor failed: %w", err)
		}
		// TODO(jakobmoellerdev): add formatting options for yaml/json
		// multiple output is equivalent to NDJSON (new line delimited json), may want array access
		if err := encoder.Encode(v2descriptor); err != nil {
			return nil, fmt.Errorf("encoding component version descriptor failed: %w", err)
		}
	}
	return buf.Bytes(), nil
}

func encodeDescriptorsAsYAML(descriptor []*descruntime.Descriptor) ([]byte, error) {
	// TODO(jakobmoellerdev): add formatting options for scheme version with v2 as only option
	v2List := make([]*v2.Descriptor, len(descriptor))
	for i, desc := range descriptor {
		v2descriptor, err := descruntime.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
		if err != nil {
			return nil, fmt.Errorf("converting component version to v2 descriptor failed: %w", err)
		}
		v2List[i] = v2descriptor
	}

	if len(v2List) == 1 {
		return yaml.Marshal(v2List[0])
	}

	return yaml.Marshal(v2List)
}
