package v2

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	_ "embed"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

// JSONSchema contains the embedded JSON schema for validating Open Component Model descriptors.
//
//go:embed resources/schema-2020-12.json
var JSONSchema []byte

// GetJSONSchema is a singleton that compiles the JSON schema once and caches it for reuse.
var GetJSONSchema = sync.OnceValues[*jsonschema.Schema, error](func() (*jsonschema.Schema, error) {
	return compile(JSONSchema)
})

// compile takes raw JSON schema data and compiles it into a jsonschema.JSONSchema object.
// It handles the compilation process including unmarshaling and resource registration.
func compile(data []byte) (*jsonschema.Schema, error) {
	const schemaFile = "resources/schema-2020-12.json"
	c := jsonschema.NewCompiler()
	unmarshaler, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}
	var v any
	if err := json.Unmarshal(JSONSchema, &v); err != nil {
		return nil, err
	}
	if err := c.AddResource(schemaFile, unmarshaler); err != nil {
		return nil, fmt.Errorf("failed to add schema: %w", err)
	}
	sch, err := c.Compile(schemaFile)
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema: %w", err)
	}
	return sch, nil
}

// Validate checks if the given descriptor conforms to the JSONSchema.
// It marshals the descriptor to JSON and validates it against the schema.
// Returns an error if validation fails or if there are issues with marshaling.
func Validate(desc *Descriptor) error {
	raw, err := json.Marshal(desc)
	if err != nil {
		return fmt.Errorf("failed to marshal descriptor: %w", err)
	}

	return ValidateRawJSON(raw)
}

// ValidateRawJSON validates raw JSON data against the Open Component Model schema.
// It unmarshals the JSON into a map and validates it against the schema.
// Returns an error if validation fails or if there are issues with unmarshaling.
func ValidateRawJSON(raw []byte) error {
	mm := map[string]any{}
	if err := json.Unmarshal(raw, &mm); err != nil {
		return fmt.Errorf("failed to unmarshal descriptor: %w", err)
	}

	schema, err := GetJSONSchema()
	if err != nil {
		return fmt.Errorf("failed to get schema: %w", err)
	}

	return schema.Validate(mm)
}

// ValidateRawYAML validates raw YAML data against the Open Component Model schema.
// It converts the YAML to JSON and validates it against the schema.
// Returns an error if validation fails or if there are issues with unmarshaling.
func ValidateRawYAML(raw []byte) error {
	mm := map[string]any{}
	if err := yaml.Unmarshal(raw, &mm); err != nil {
		return fmt.Errorf("failed to unmarshal descriptor: %w", err)
	}

	schema, err := GetJSONSchema()
	if err != nil {
		return fmt.Errorf("failed to get schema: %w", err)
	}

	return schema.Validate(mm)
}

// ValidateVersions checks the component version and every resource, source, and
// reference version against the given versioning registry. It is separate from
// [Validate] (which only performs structural JSON-schema validation) because
// version-scheme validity is configuration-driven and cannot be expressed in the
// static schema. It returns a joined error naming each offending field.
func ValidateVersions(desc *Descriptor, registry *versioning.Registry) error {
	if registry == nil {
		registry = versioning.Default()
	}

	var errs []error
	check := func(kind, name, version string) {
		if !registry.Valid(version) {
			errs = append(errs, fmt.Errorf("%s %q has an invalid version %q for the configured versioning schemes", kind, name, version))
		}
	}

	check("component", desc.Component.Name, desc.Component.Version)
	for _, r := range desc.Component.Resources {
		check("resource", r.Name, r.Version)
	}
	for _, s := range desc.Component.Sources {
		check("source", s.Name, s.Version)
	}
	for _, ref := range desc.Component.References {
		check("reference", ref.Name, ref.Version)
	}

	return errors.Join(errs...)
}
