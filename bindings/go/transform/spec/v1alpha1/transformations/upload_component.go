package transformations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

const UploadComponentTransformationType = "ocm.software.upload.component"

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type UploadComponentTransformation struct {
	meta.TransformationMeta `json:",inline"`
	Spec                    UploadComponentTransformationSpec `json:"spec"`

	Provider repository.ComponentVersionRepositoryProvider `json:"-"`
}

func (t *UploadComponentTransformation) GetTransformationMeta() *meta.TransformationMeta {
	return &t.TransformationMeta
}

type UploadComponentTransformationSpec struct {
	Repository *runtime.Raw   `json:"repository"`
	Descriptor *v2.Descriptor `json:"descriptor"`
}

func (in *UploadComponentTransformationSpec) DeepCopyInto(out *UploadComponentTransformationSpec) {
	*out = *in
}

//func (*UploadComponentTransformation) NestedTypedFields() []string {
//	return []string{"repository"}
//}

// 1) Get this plugin to run with a ctf
// 2) Implement upload transformation for an e2e scenario
// 3) Decouple transformation from plugin manager with shared contracts

// Transform downloads the specified component version descriptor from the given repository.
// We might want to investigate returning cel.Activation here to allow for more flexibility
func (t *UploadComponentTransformation) Transform(ctx context.Context, credentialProvider credentials.Resolver) (map[string]any, error) {
	consumerId, err := t.Provider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, t.Spec.Repository)
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
	repo, err := t.Provider.GetComponentVersionRepository(ctx, t.Spec.Repository, creds)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %v", err)
	}
	desc, err := descruntime.ConvertFromV2(t.Spec.Descriptor)
	if err != nil {
		return nil, fmt.Errorf("failed to convert descriptor to runtime format: %v", err)
	}
	// TODO(fabianburth): throw an error if one attempts to marshal a runtime
	//  descriptor
	if err := repo.AddComponentVersion(ctx, desc); err != nil {
		return nil, fmt.Errorf("failed to upload component version %s:%s to %v: %v", t.Spec.Descriptor.Component.Name, t.Spec.Descriptor.Component.Version, t.Spec.Repository, err)
	}
	return nil, nil
}

func (t *UploadComponentTransformation) FromGeneric(generic *v1alpha1.GenericTransformation) error {
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

	data, err = json.Marshal(generic.Spec.Data["descriptor"])
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}
	var descriptor v2.Descriptor
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&descriptor); err != nil {
		return fmt.Errorf("failed to decode strict into v2 descriptor: %w", err)
	}
	transformation := &UploadComponentTransformation{
		TransformationMeta: generic.TransformationMeta,
		Spec: UploadComponentTransformationSpec{
			Repository: &raw,
			Descriptor: &descriptor,
		},
	}
	t.TransformationMeta = transformation.TransformationMeta
	t.Spec = transformation.Spec
	return nil
}

//func (t *UploadComponentTransformation) NewDeclType(nestedFieldTypes map[string]runtime.Type) (*stv6jsonschema.DeclType, error) {
//	repoFieldType, ok := nestedFieldTypes["repository"]
//	if !ok {
//		return nil, fmt.Errorf("missing nested field type for spec.repository")
//	}
//
//	specSchema, outSchema, err := uploadComponentTransformationJSONSchema(t.Provider, repoFieldType)
//	if err != nil {
//		return nil, fmt.Errorf("get JSON schema for %s: %w", repoFieldType.String(), err)
//	}
//	s := &jsonschema.Schema{
//		Required: []string{"spec", "output"},
//	}
//	s.Types.Add("object")
//	s.Properties = make(map[string]*jsonschema.Schema)
//	s.Properties["spec"] = specSchema
//	s.Properties["output"] = outSchema
//
//	decl := stv6jsonschema.NewSchemaDeclType(s)
//	specfield := decl.Fields["spec"]
//	descriptorField := specfield.Type.Fields["descriptor"]
//	descriptorField.Type = descriptorField.Type.MaybeAssignTypeName("iloveinvopop")
//	specfield.Type.Fields["descriptor"] = descriptorField
//	decl.Fields["spec"] = specfield
//	return decl, nil
//}
//
//func uploadComponentTransformationJSONSchema(
//	provider repository.ComponentVersionRepositoryProvider,
//	typ runtime.Type,
//) (*invopop.Schema, *invopop.Schema, error) {
//	// first convert repos
//	repoSchema, err := provider.GetJSONSchema(context.TODO(), typ)
//	if err != nil {
//		return nil, nil, fmt.Errorf("failed to get JSON schema for repository %s: %w", typ, err)
//	}
//	repoInvopop := invopop.Schema{}
//	if err := json.Unmarshal(repoSchema, &repoInvopop); err != nil {
//		return nil, nil, fmt.Errorf("failed to unmarshal JSON schema for repository %s: %w", typ, err)
//	}
//	reflector := invopop.Reflector{
//		DoNotReference: true,
//		Anonymous:      true,
//		IgnoredTypes:   []any{&runtime.Raw{}},
//	}
//	transformationSpecJSONSchema := reflector.Reflect(&UploadComponentTransformationSpec{})
//	transformationSpecJSONSchema.Properties.Set("repository", &repoInvopop)
//	descriptorSchema, err := v2.GetJSONSchema()
//	if err != nil {
//		return nil, nil, fmt.Errorf("failed to get JSON schema for descriptor: %w", err)
//	}
//	transformationSpecJSONSchema.Properties.Set("descriptor", &descriptorSchema.Invopop)
//	return transformationSpecJSONSchema, nil, nil
//}
