package docs

import "ocm.software/open-component-model/bindings/go/runtime/docs/diataxis"

// XDocs is an extension of schemas that can be used to extend it with documentation-awareness.
// Currently we only support Diataxis.
//
// +ocm:jsonschema-gen=true
type XDocs struct {
	Diataxis diataxis.Diataxis `json:"diataxis,omitempty"`
}

// +ocm:jsonschema-gen=true
type Schema struct {
	XDocs XDocs `json:"x-docs"`
}
