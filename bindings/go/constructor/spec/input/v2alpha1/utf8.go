package v2alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// UTF8 describes an input inlined with UTF8 encoded
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type UTF8 struct {
	Type runtime.Type `json:"type"`
	// Path is the path to the file.
	Text         string           `json:"text,omitempty"`
	Object       any              `json:"object,omitempty"`
	ObjectFormat UTF8ObjectFormat `json:"objectFormat,omitempty"`
	Compress     bool             `json:"compress,omitempty"`
	MediaType    string           `json:"mediaType,omitempty"`
}

func (t *UTF8) HasText() bool {
	return t.Text != ""
}

func (t *UTF8) HasObject() bool {
	return t.Object != nil
}

type UTF8ObjectFormat string

const (
	// UTF8ObjectFormatJSON indicates marshalling the object to JSON
	UTF8ObjectFormatJSON UTF8ObjectFormat = "json"
	// UTF8ObjectFormatYAML indicates marshalling the object to YAML
	UTF8ObjectFormatYAML UTF8ObjectFormat = "yaml"
)

func (t *UTF8) String() string {
	return t.Text
}
