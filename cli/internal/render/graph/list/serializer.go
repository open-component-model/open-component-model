package list

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"

	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/cli/internal/render"
	"sigs.k8s.io/yaml"
)

// Serializer implements the ListSerializer interface for serializing
// vertices to JSON format.
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
	Serialize(vertex *syncdag.Vertex[T]) (any, error)
}

// VertexSerializerFunc is a function type that implements the VertexSerializer
// interface.
type VertexSerializerFunc[T cmp.Ordered] func(vertex *syncdag.Vertex[T]) (any, error)

func (f VertexSerializerFunc[T]) Serialize(vertex *syncdag.Vertex[T]) (any, error) {
	return f(vertex)
}

func NewSerializer[T cmp.Ordered](opts ...SerializerOption[T]) Serializer[T] {
	options := SerializerOptions[T]{}
	for _, opt := range opts {
		opt(&options)
	}
	if options.VertexSerializer == nil {
		options.VertexSerializer = VertexSerializerFunc[T](func(vertex *syncdag.Vertex[T]) (any, error) {
			return fmt.Sprintf("%v", vertex.ID), nil
		})
	}
	if options.OutputFormat == 0 {
		options.OutputFormat = render.OutputFormatJSON
	}
	return Serializer[T]{
		VertexSerializer: options.VertexSerializer,
		OutputFormat:     options.OutputFormat,
	}
}

func (s Serializer[T]) Serialize(writer io.Writer, vertices []*syncdag.Vertex[T]) error {
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
	}
	return nil
}
