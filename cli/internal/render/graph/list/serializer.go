package list

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"

	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/dag"
	"ocm.software/open-component-model/cli/internal/render"
)

// Serializer implements the ListSerializer interface for serializing
// a slice of vertices to a set of output formats.
type Serializer[T cmp.Ordered] struct {
	// VertexSerializer is a function that converts a vertex to an object
	// that can be serialized to JSON.
	VertexSerializer VertexSerializer[T]
	// OutputFormat specifies the format in which the output should be rendered.
	// Serializer supports JSON, NDJSON, and YAML formats.
	OutputFormat render.OutputFormat
}

type VertexSerializer[T cmp.Ordered] interface {
	// Serialize converts a vertex to an object that can be serialized.
	Serialize(vertex *dag.Vertex[T]) (any, error)
}

// VertexSerializerFunc is a function type that implements the VertexSerializer
// interface.
type VertexSerializerFunc[T cmp.Ordered] func(vertex *dag.Vertex[T]) (any, error)

func (f VertexSerializerFunc[T]) Serialize(vertex *dag.Vertex[T]) (any, error) {
	return f(vertex)
}

func NewSerializer[T cmp.Ordered](opts ...SerializerOption[T]) Serializer[T] {
	serializer := Serializer[T]{}
	for _, opt := range opts {
		opt(&serializer)
	}
	if serializer.VertexSerializer == nil {
		serializer.VertexSerializer = VertexSerializerFunc[T](func(vertex *dag.Vertex[T]) (any, error) {
			return fmt.Sprintf("%v", vertex.ID), nil
		})
	}
	if serializer.OutputFormat == 0 {
		serializer.OutputFormat = render.OutputFormatJSON
	}
	return serializer
}

func (s Serializer[T]) Serialize(writer io.Writer, vertices []*dag.Vertex[T]) error {
	var list []any
	for _, v := range vertices {
		obj, err := s.VertexSerializer.Serialize(v)
		if err != nil {
			return fmt.Errorf("failed to serialize vertex %v: %w", v.ID, err)
		}
		list = append(list, obj)
	}
	switch s.OutputFormat {
	case render.OutputFormatJSON:
		data, err := json.MarshalIndent(list, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling vertices to JSON failed: %w", err)
		}
		data = append(data, '\n')

		if _, err = writer.Write(data); err != nil {
			return fmt.Errorf("writing JSON data to writer failed: %w", err)
		}
	case render.OutputFormatNDJSON:
		encoder := json.NewEncoder(writer)
		for _, v := range list {
			if err := encoder.Encode(v); err != nil {
				return fmt.Errorf("encoding component version descriptor failed: %w", err)
			}
		}
	case render.OutputFormatYAML:
		data, err := yaml.Marshal(list)
		if err != nil {
			return fmt.Errorf("marshalling vertices to YAML failed: %w", err)
		}
		if _, err = writer.Write(data); err != nil {
			return fmt.Errorf("writing YAML data to writer failed: %w", err)
		}
	default:
		return fmt.Errorf("unknown output format: %q", s.OutputFormat)
	}
	return nil
}
