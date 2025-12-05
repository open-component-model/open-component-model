package transformations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
	"ocm.software/open-component-model/bindings/go/credentials"
	v2runtime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

const DownloadComponentTransformationType = "ocm.software.download.component"

type ComponentVersionDownloadTransformation struct {
	declType     *stv6jsonschema.DeclType
	repoProvider repository.ComponentVersionRepositoryProvider
}

func (t *ComponentVersionDownloadTransformation) GetType() runtime.Type {
	return runtime.NewUnversionedType(DownloadComponentTransformationType + ".oci")
}

func (t *ComponentVersionDownloadTransformation) GetDeclType() *stv6jsonschema.DeclType {
	return t.declType
}

type loader struct {
	schema any
}

func (l loader) Load(url string) (any, error) {
	return nil, fmt.Errorf("loader: no such resource %s", url)
}

func NewOCIComponentVersionDownloadTransformation(
	repo repository.ComponentVersionRepositoryProvider,
) (*ComponentVersionDownloadTransformation, error) {
	transformationJSON, err := jsonschema.UnmarshalJSON(bytes.NewReader(DownloadComponentTransformation{}.JSONSchema()))
	if err != nil {
		return nil, err
	}

	repoJSON, err := jsonschema.UnmarshalJSON(bytes.NewReader(oci.Repository{}.JSONSchema()))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	//compiler.UseLoader(loader{transformationJSON})

	defs := transformationJSON.(map[string]any)["$defs"]
	for key, def := range defs.(map[string]any) {
		//id := def.(map[string]any)["$id"].(string)
		if err := compiler.AddResource(key, def); err != nil {
			return nil, err
		}
	}

	if err := compiler.AddResource("DownloadComponentTransformation.schema.json", transformationJSON); err != nil {
		return nil, err
	}
	if err := compiler.AddResource("Repository.schema.json", repoJSON); err != nil {
		return nil, err
	}

	base := &ComponentVersionDownloadTransformation{
		repoProvider: repo,
	}

	schema, err := compiler.Compile("DownloadComponentTransformation.schema.json")
	if err != nil {
		return nil, err
	}
	schema.Properties["type"].Enum = &jsonschema.Enum{Values: []any{base.GetType().String()}}

	reposchema, err := compiler.Compile("Repository.schema.json")
	if err != nil {
		return nil, err
	}
	schema.Properties["spec"].Ref.Properties["repository"] = reposchema

	v2descriptor, err := v2.GetJSONSchema()
	if err != nil {
		return nil, err
	}
	schema.Properties["output"] = v2descriptor

	base.declType = stv6jsonschema.NewSchemaDeclType(schema)

	return base, nil
}

func (t *ComponentVersionDownloadTransformation) Transform(
	ctx context.Context,
	step *v1alpha1.GenericTransformation,
	credentialProvider credentials.Resolver,
) (*v1alpha1.GenericTransformation, error) {
	transformation := &DownloadComponentTransformation{}
	if err := transformation.FromGeneric(step); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to download component transformation: %v", err)
	}

	consumerId, err := t.repoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, transformation.Spec.Repository)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository credential consumer identity: %v", err)
	}
	var creds map[string]string
	if credentialProvider != nil {
		creds, err = credentialProvider.Resolve(ctx, consumerId)
		if err != nil {
			return nil, fmt.Errorf("failed resolving credentials: %v", err)
		}
	}

	repo, err := t.repoProvider.GetComponentVersionRepository(ctx, transformation.Spec.Repository, creds)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %v", err)
	}
	// TODO(fabianburth): throw an error if one attempts to marshal a runtime
	//  descriptor
	desc, err := repo.GetComponentVersion(ctx, transformation.Spec.Component, transformation.Spec.Version)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version %s:%s: %v", transformation.Spec.Component, transformation.Spec.Version, err)
	}

	v2desc, err := v2runtime.ConvertToV2(runtime.NewScheme(), desc)
	if err != nil {
		return nil, fmt.Errorf("failed converting component version to v2: %v", err)
	}
	var m map[string]any
	data, err := json.Marshal(v2desc)
	if err != nil {
		return nil, fmt.Errorf("failed marshalling component version descriptor: %v", err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed unmarshalling component version descriptor into map: %v", err)
	}

	// TODO remove hack
	step.Output = &runtime.Unstructured{
		Data: map[string]interface{}{
			"descriptor": m,
		},
	}

	return step, nil
}

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type DownloadComponentTransformation struct {
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
	Repository *runtime.Raw `json:"repository"`
	Component  string       `json:"component"`
	Version    string       `json:"version"`
}

func (t *DownloadComponentTransformation) FromGeneric(generic *v1alpha1.GenericTransformation) error {
	data, err := json.Marshal(generic.Spec.Data["repository"])
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}
	var raw runtime.Raw
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return fmt.Errorf("failed to decode strict into runtime raw: %w", err)
	}
	transformation := &DownloadComponentTransformation{
		Type: generic.Type,
		ID:   generic.ID,
		Spec: &DownloadComponentTransformationSpec{
			Repository: &raw,
			Component:  generic.Spec.Data["component"].(string),
			Version:    generic.Spec.Data["version"].(string),
		},
		Output: nil,
	}
	t.Spec = transformation.Spec
	return nil
}

//
//func downloadComponentTransformationJSONSchema(
//	provider repository.ComponentVersionRepositoryProvider,
//	typ runtime.Type,
//) (*invopop.Schema, *invopop.Schema, error) {
//	// first convert repos
//	repoSchema, err := provider.GetJSONSchema(context.TODO(), typ)
//	if err != nil {
//		return nil, nil, fmt.Errorf("failed to get JSON schema for repository %s: %w", typ, err)
//	}
//	repoInvopop := &invopop.Schema{}
//	if err := repoInvopop.UnmarshalJSON(repoSchema); err != nil {
//		return nil, nil, fmt.Errorf("failed to unmarshal JSON schema for repository %s: %w", typ, err)
//	}
//	reflector := invopop.Reflector{
//		DoNotReference: true,
//		Anonymous:      true,
//		IgnoredTypes:   []any{&runtime.Raw{}},
//	}
//	transformationSpecJSONSchema := reflector.Reflect(&DownloadComponentTransformationSpec{})
//	transformationSpecJSONSchema.Properties.Set("repository", repoInvopop)
//	descriptorSchema, err := v2.GetJSONSchema()
//	if err != nil {
//		return nil, nil, fmt.Errorf("failed to get JSON schema for descriptor: %w", err)
//	}
//	return transformationSpecJSONSchema, &descriptorSchema.Invopop, nil
//}
