package codec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// TODO: Evaluate where we even need this, the plugin knows the type it wants
//  to unmarshal into anyway.

// Decoder is any arbitrary object that can decode objects into a given
// struct through go tags. An example is encoding/json
type Decoder interface {
	Decode(v any) error
}

type Encoder interface {
	Encode(v any) error
}

type TypedDecoderFactory interface {
	NewTypedDecoder(reader io.Reader) TypedDecoder
}

// TypedDecoder is a decoder that converts into a types.Typed
// based on underlying typing rules. As such it is more concrete
// than Decoder in that it decides on the type to parse and the instantiation
// itself.
type TypedDecoder interface {
	Decode() (runtime.Typed, error)
}

func NewJSONDecoder(reader io.Reader) (Decoder, error) {
	return json.NewDecoder(reader), nil
}

func NewYAMLDecoder(reader io.Reader) (Decoder, error) {
	return &YAMLDecoder{data: reader}, nil
}

type YAMLDecoder struct {
	data io.Reader
}

func (d *YAMLDecoder) Decode(v any) error {
	b, err := io.ReadAll(d.data)
	if err != nil {
		return fmt.Errorf("could not read data: %w", err)
	}
	return yaml.Unmarshal(b, v)
}

// NewTypedDecoderFactory creates a new TypedDecoder backed by a registry.
// TODO: pass entire configuration instead of just the registry?
func NewTypedDecoderFactory(registry *runtime.Scheme, decoder func(reader io.Reader) (Decoder, error)) TypedDecoderFactory {
	return &RegistryDecoderFactory{
		decoder:  decoder,
		registry: registry,
	}
}

type RegistryDecoderFactory struct {
	decoder  func(reader io.Reader) (Decoder, error)
	registry *runtime.Scheme
}

func (f *RegistryDecoderFactory) NewTypedDecoder(reader io.Reader) TypedDecoder {
	return &RegistryDecoder{
		reader:   reader,
		decoder:  f.decoder,
		registry: f.registry,
	}
}

// RegistryDecoder is a TypedDecoder backed by a registry.Scheme
// to do typing decisions
type RegistryDecoder struct {
	reader   io.Reader
	decoder  func(io.Reader) (Decoder, error)
	registry *runtime.Scheme
}

func (d *RegistryDecoder) Decode() (runtime.Typed, error) {
	// buffer the data so we can read it twice
	reader := d.reader
	var buf bytes.Buffer
	reader = io.TeeReader(reader, &buf)
	decoder, err := d.decoder(reader)
	if err != nil {
		return nil, fmt.Errorf("could not create decoder for type discovery: %w", err)
	}

	// Extract the type to decide the concrete implementation
	var generic struct {
		Type runtime.Type `json:"type"`
	}
	if err = decoder.Decode(&generic); err != nil {
		return nil, err
	}
	if generic.Type.GetType() == "" {
		return nil, fmt.Errorf("missing or invalid 'type' in object that was expected to be typed")
	}

	instance, err := d.registry.NewObject(generic.Type)
	if err != nil {
		return nil, fmt.Errorf("failed to create new object of type %s: %w", generic.Type, err)
	}

	// reset the reader to the beginning of the data
	reader = io.MultiReader(&buf, reader)
	decoder, err = d.decoder(reader)
	if err != nil {
		return nil, fmt.Errorf("could not create decoder for typed decoding: %w", err)
	}

	// Decode the YAML node directly into the specific Typed instance
	if err = decoder.Decode(instance); err != nil {
		return nil, err
	}

	return instance, nil
}
