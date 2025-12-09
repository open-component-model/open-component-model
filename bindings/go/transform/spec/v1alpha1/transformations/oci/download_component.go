package oci

import (
	"bytes"

	"github.com/santhosh-tekuri/jsonschema/v6"
	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

var DownloadComponentTransformationType = runtime.NewUnversionedType("ocm.software.download.component.oci")

func (t *DownloadComponentTransformation) GetDeclType() (*stv6jsonschema.DeclType, error) {
	transformationJSON, err := jsonschema.UnmarshalJSON(bytes.NewReader(DownloadComponentTransformation{}.JSONSchema()))
	if err != nil {
		return nil, err
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("transformation.schema.json", transformationJSON); err != nil {
		return nil, err
	}
	schema, err := compiler.Compile("transformation.schema.json")
	if err != nil {
		return nil, err
	}
	v2descriptor, err := v2.GetJSONSchema()
	if err != nil {
		return nil, err
	}
	schema.Properties["output"] = v2descriptor
	return stv6jsonschema.NewSchemaDeclType(schema), nil
}

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type DownloadComponentTransformation struct {
	// +ocm:jsonschema-gen:enum=ocm.software.download.component.oci/v1alpha1
	Type   runtime.Type                           `json:"type"`
	ID     string                                 `json:"id"`
	Spec   *DownloadComponentTransformationSpec   `json:"spec"`
	Output *DownloadComponentTransformationOutput `json:"output,omitempty"`
}

func (t *DownloadComponentTransformation) GetTransformationMeta() *meta.TransformationMeta {
	return &meta.TransformationMeta{Type: t.Type, ID: t.ID}
}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type DownloadComponentTransformationOutput struct {
	Descriptor *v2.Descriptor `json:"descriptor"`
}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type DownloadComponentTransformationSpec struct {
	Repository *oci.Repository `json:"repository"`
	Component  string          `json:"component"`
	Version    string          `json:"version"`
}
