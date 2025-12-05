package transformations

import (
	"bytes"
	"encoding/json"
	"fmt"

	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

type Transformation interface {
	Transform(step *runtime.Unstructured) (*runtime.Unstructured, error)
}

type TransformationRegistry struct {
	// runtime.Type is the transformation type (e.g. component.download.oci)
	transformations map[runtime.Type]Transformation
	// transformations map[runtime.Type]Transformation
	// holds n instances of e.g. ComponentVersionDownloadTransformation
	// 1 instance per plugin (oci, ctf)
}

type ComponentVersionDownloadTransformation struct {
	schema   stv6jsonschema.Schema
	declType *stv6jsonschema.DeclType
	repo     repository.ComponentVersionRepositoryProvider
}

func NewTransformation(repo repository.ComponentVersionRepositoryProvider, declType *stv6jsonschema.DeclType) *ComponentVersionDownloadTransformation {
	return nil
}

func (t *ComponentVersionDownloadTransformation) Transform(step *runtime.Unstructured) (*runtime.Unstructured, error) {
	return nil, nil
}

const DownloadComponentTransformationType = "ocm.software.download.component"

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type DownloadComponentTransformation struct {
	meta.TransformationMeta `json:",inline"`
	Spec                    DownloadComponentTransformationSpec `json:"spec"`

	Provider repository.ComponentVersionRepositoryProvider `json:"-"`
}

func (t *DownloadComponentTransformation) GetTransformationMeta() *meta.TransformationMeta {
	return &t.TransformationMeta
}

// +k8s:deepcopy-gen=true
type DownloadComponentTransformationSpec struct {
	Repository *runtime.Raw `json:"repository"`
	Component  string       `json:"component"`
	Version    string       `json:"version"`
}

//func (*DownloadComponentTransformation) NestedTypedFields() []string {
//	return []string{"repository"}
//}
//
// 1) Get this plugin to run with a ctf
// 2) Implement upload transformation for an e2e scenario
// 3) Decouple transformation from plugin manager with shared contracts

// Transform downloads the specified component version descriptor from the given repository.
// We might want to investigate returning cel.Activation here to allow for more flexibility
//
//	func (t *DownloadComponentTransformation) Transform(ctx context.Context, credentialProvider credentials.GraphResolver) (map[string]any, error) {
//		consumerId, err := t.Provider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, t.Spec.Repository)
//		if err != nil {
//			return nil, fmt.Errorf("failed getting component version repository credential consumer identity: %v", err)
//		}
//		var creds map[string]string
//		if credentialProvider != nil {
//			creds, err = credentialProvider.Resolve(ctx, consumerId)
//			if err != nil {
//				return nil, fmt.Errorf("failed resolving credentials: %v", err)
//			}
//		}
//		repo, err := t.Provider.GetComponentVersionRepository(ctx, t.Spec.Repository, creds)
//		if err != nil {
//			return nil, fmt.Errorf("failed getting component version repository: %v", err)
//		}
//		// TODO(fabianburth): throw an error if one attempts to marshal a runtime
//		//  descriptor
//		desc, err := repo.GetComponentVersion(ctx, t.Spec.Component, t.Spec.Version)
//		if err != nil {
//			return nil, fmt.Errorf("failed getting component version %s:%s: %v", t.Spec.Component, t.Spec.Version, err)
//		}
//
//		v2desc, err := v2runtime.ConvertToV2(runtime.NewScheme(), desc)
//		if err != nil {
//			return nil, fmt.Errorf("failed converting component version to v2: %v", err)
//		}
//		var m map[string]any
//		data, err := json.Marshal(v2desc)
//		if err != nil {
//			return nil, fmt.Errorf("failed marshalling component version descriptor: %v", err)
//		}
//		if err := json.Unmarshal(data, &m); err != nil {
//			return nil, fmt.Errorf("failed unmarshalling component version descriptor into map: %v", err)
//		}
//		return m, nil
//	}
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
		TransformationMeta: generic.TransformationMeta,
		Spec: DownloadComponentTransformationSpec{
			Repository: &raw,
			Component:  generic.Spec.Data["component"].(string),
			Version:    generic.Spec.Data["version"].(string),
		},
	}
	t.TransformationMeta = transformation.TransformationMeta
	t.Spec = transformation.Spec
	return nil
}

//
//func (t *DownloadComponentTransformation) NewDeclType(nestedFieldTypes map[string]runtime.Type) (*jsonschema.DeclType, error) {
//	repoFieldType, ok := nestedFieldTypes["repository"]
//	if !ok {
//		return nil, fmt.Errorf("missing nested field type for spec.repository")
//	}
//
//	specSchema, outSchema, err := downloadComponentTransformationJSONSchema(t.Provider, repoFieldType)
//	if err != nil {
//		return nil, fmt.Errorf("get JSON schema for %s: %w", repoFieldType.String(), err)
//	}
//	s := &invopop.Schema{
//		Type:       "object",
//		Properties: invopop.NewProperties(),
//		Required:   []string{"spec", "output"},
//	}
//	s.Properties.Set("spec", specSchema)
//	s.Properties.Set("output", outSchema)
//
//	decl := jsonschema.DeclTypeFromInvopop(s)
//	decl.Fields["output"].Type = decl.Fields["output"].Type.MaybeAssignTypeName("iloveinvopop")
//	decl = decl.MaybeAssignTypeName("__type_" + t.ID)
//	return decl, nil
//}
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
