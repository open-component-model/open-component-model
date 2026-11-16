package testutils

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

type MockObject struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	Content string `json:"content,omitempty"`
}

// MockCustomSchemaObject is a mock object to test a set of known
// JSONSchema edge cases.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
// +ocm:jsonschema-gen:schema-from=schemas/custom_schema.json
type MockCustomSchemaObject struct {
	StringWithPattern string `json:"stringWithPattern,omitempty"`
	OneOfStringOrNull string `json:"oneOfStringOrNull,omitempty"`
	// No omitempty: the zero value marshals as "" via MarshalJSONTo, which encoding/json v1
	// always emitted (omitempty never applied to structs) and which v2's omitempty would drop.
	OneOfStringNumberOrNull OneOfStringNumberOrNull `json:"oneOfStringNumberOrNull"`
}

// OneOfStringNumberOrNull is a mock type to test oneOf with multiple types.
// +k8s:deepcopy-gen=true
type OneOfStringNumberOrNull struct {
	String *string `json:"string,omitzero"`
	Number *int    `json:"number,omitzero"`
}

func (o OneOfStringNumberOrNull) MarshalJSONTo(enc *jsontext.Encoder) error {
	if o.String != nil {
		return json.MarshalEncode(enc, *o.String, runtime.JSONOptions)
	}
	if o.Number != nil {
		return json.MarshalEncode(enc, *o.Number, runtime.JSONOptions)
	}
	return enc.WriteToken(jsontext.String(""))
}

func (o *OneOfStringNumberOrNull) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	data, err := dec.ReadValue()
	if err != nil {
		return err
	}
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if stringVal := ""; json.Unmarshal(data, &stringVal) == nil {
		o.String = &stringVal
		return nil
	}
	if intVal := 0; json.Unmarshal(data, &intVal) == nil {
		o.Number = &intVal
		return nil
	}
	return fmt.Errorf("unknown json value: %s", string(data))
}
