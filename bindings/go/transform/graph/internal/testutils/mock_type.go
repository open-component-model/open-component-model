package testutils

import (
	"encoding/json"
	"fmt"
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
	StringWithPattern       string                  `json:"stringWithPattern,omitempty"`
	OneOfStringOrNull       string                  `json:"oneOfStringOrNull,omitempty"`
	OneOfStringNumberOrNull OneOfStringNumberOrNull `json:"oneOfStringNumberOrNull,omitempty"`
}

// OneOfStringNumberOrNull is a mock type to test oneOf with multiple types.
// +k8s:deepcopy-gen=true
type OneOfStringNumberOrNull struct {
	String *string `json:"string,omitempty"`
	Number *int    `json:"number,omitempty"`
}

func (o OneOfStringNumberOrNull) MarshalJSON() ([]byte, error) {
	if o.String != nil {
		return json.Marshal(*o.String)
	}
	if o.Number != nil {
		return json.Marshal(*o.Number)
	}
	return []byte(`""`), nil
}

func (o *OneOfStringNumberOrNull) UnmarshalJSON(data []byte) error {
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
