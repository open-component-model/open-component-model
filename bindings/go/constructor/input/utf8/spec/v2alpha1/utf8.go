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
	// Text is the UTF8 encoded text.
	Text string `json:"text,omitempty"`
	// Object is the object to be marshalled to UTF8, mutually exclusive with Text.
	Object *runtime.Unstructured `json:"object,omitempty"`
	// ObjectFormat is the format to use when marshalling Object to UTF8, mutually exclusive with Text.
	ObjectFormat UTF8ObjectFormat `json:"objectFormat,omitempty"`
	// Compress indicates whether the UTF8 encoded text should be compressed with gzip.
	Compress bool `json:"compress,omitempty"`
	// MediaType is the media type of the UTF8 encoded text. If not specified and ObjectFormat is set,
	// the media type will be defaulted to either application/json (UTF8ObjectFormatJSON) or application/x-yaml (UTF8ObjectFormatYAML).
	// If not specified and Text is set, the media type will be default to text/plain.
	MediaType string `json:"mediaType,omitempty"`
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
