package v1

import (
	"encoding/json"
	"errors"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	Version = "v1"
	Type    = "utf8"
)

// UTF8 describes an input sourced by a UTF-8 string.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type UTF8 struct {
	// +ocm:jsonschema-gen:enum=utf8/v1,UTF8/v1
	// +ocm:jsonschema-gen:enum:deprecated=utf8,UTF8
	Type runtime.Type `json:"type"`
	// Text is an UTF-8 string, raw encoded.
	Text string `json:"text,omitempty"`
	// JSON is a JSON object, raw encoded via UTF-8.
	JSON json.RawMessage `json:"json,omitempty"`
	// FormattedJSON is a JSON object, raw encoded via UTF-8, with default indentation applied.
	FormattedJSON json.RawMessage `json:"formattedJson,omitempty"`
	// YAML is a YAML object, raw encoded via UTF-8.
	YAML json.RawMessage `json:"yaml,omitempty"`
	// Compress indicates whether the file should be compressed with gzip.
	Compress bool `json:"compress,omitempty"`
}

func (t *UTF8) Validate() error {
	// only one of Text, JSON, or YAML should be set
	if t.Text == "" && len(t.JSON) == 0 && len(t.YAML) == 0 && len(t.FormattedJSON) == 0 {
		return errors.New("one of 'text', 'json', 'formattedJson', or 'yaml' must be set")
	}
	if t.Text != "" && (len(t.JSON) > 0 || len(t.YAML) > 0 || len(t.FormattedJSON) > 0) ||
		len(t.JSON) > 0 && (t.Text != "" || len(t.YAML) > 0 || len(t.FormattedJSON) > 0) ||
		len(t.YAML) > 0 && (t.Text != "" || len(t.JSON) > 0 || len(t.FormattedJSON) > 0) ||
		len(t.FormattedJSON) > 0 && (t.Text != "" || len(t.JSON) > 0 || len(t.YAML) > 0) {
		return errors.New("only one of 'text', 'json', 'formattedJson', or 'yaml' can be set")
	}
	return nil
}
