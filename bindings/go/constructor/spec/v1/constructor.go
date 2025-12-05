package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/runtime"
)

type ComponentConstructor struct {
	Components []Component `json:"components"`
}

func (c *ComponentConstructor) UnmarshalJSON(data []byte) error {
	if err := ValidateRawJSON(data); err != nil {
		return fmt.Errorf("component constructor validation failed: %w", err)
	}

	return c.UnmarshalJSONUnsafe(data)
}

// UnmarshalJSONUnsafe is a variant of UnmarshalJSON that does not perform schema validation.
// It is intended for use cases where partial or potentially invalid data needs to be processed.
func (c *ComponentConstructor) UnmarshalJSONUnsafe(data []byte) error {
	// Try to unmarshal as a struct with "components" field (array form)
	type Alias ComponentConstructor
	var alias Alias // use an alias because that won't cause recursion into UnmarshalJSON
	var errs []error

	if err := json.Unmarshal(data, &alias); err == nil && len(alias.Components) > 0 {
		c.Components = alias.Components
		return nil
	} else {
		errs = append(errs, err)
	}

	// Try to unmarshal as a single component (object form)
	var single Component
	if err := json.Unmarshal(data, &single); err == nil {
		c.Components = []Component{single}
		return nil
	} else {
		errs = append(errs, err)
	}

	// If both fail, return an error
	return errors.Join(errs...)
}

// These constants describe identity attributes predefined by the
// model used to identify elements (resources, sources and references)
// in a component version.
const (
	IdentityAttributeName    = "name"
	IdentityAttributeVersion = "version"
)

// Component defines a named and versioned component containing dependencies such as sources, resources and
// references pointing to further component versions.
type Component struct {
	ComponentMeta `json:",inline"`
	// Provider describes the provider type of component in the origin's context.
	Provider Provider `json:"provider"`
	// Resources defines all resources that are created by the component and by a third party.
	Resources []Resource `json:"resources"`
	// Sources defines sources that produced the component.
	Sources []Source `json:"sources"`
	// References component dependencies that can be resolved in the current context.
	References []Reference `json:"componentReferences"`
}

type Provider struct {
	Name   string  `json:"name"`
	Labels []Label `json:"labels,omitempty"`
}

// ResourceRelation describes whether the component is created by a third party or internally.
type ResourceRelation string

const (
	// LocalRelation defines a internal relation
	// which describes a internally maintained resource in the origin's context.
	LocalRelation ResourceRelation = "local"
	// ExternalRelation defines a external relation
	// which describes a resource maintained by a third party vendor in the origin's context.
	ExternalRelation ResourceRelation = "external"
)

// A Resource is a delivery artifact, intended for deployment into a runtime environment, or describing additional content,
// relevant for a deployment mechanism.
//
// In a component constructor, the Resource is defined with an AccessOrInput that specifies if
//
//   - the resource is a resource that is just externally referenced (Access)
//   - the resource is a resource that is embedded / added during component constructor (Input)
//
// For example, installation procedures or meta-model descriptions controlling orchestration and/or deployment mechanisms.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/02-elements-toplevel.md#resources
// +k8s:deepcopy-gen=true
type Resource struct {
	ElementMeta `json:",inline"`
	// SourceRefs defines a list of source names.
	// These entries reference the sources defined in the
	// component.sources.
	SourceRefs []SourceRef `json:"sourceRefs,omitempty"`
	// Type describes the type of the object.
	Type string `json:"type"`
	// Relation describes the relation of the resource to the component.
	// Can be a local or external resource.
	Relation ResourceRelation `json:"relation"`

	AccessOrInput `json:",inline"`

	ConstructorAttributes `json:",inline"`
}

// ConstructorAttributes defines additional attributes used in a component constructor context.
// +k8s:deepcopy-gen=true
type ConstructorAttributes struct {
	CopyPolicy `json:"copyPolicy,omitempty"`
}

// CopyPolicy defines how the given object should be added during the component construction.
// If set to "reference", the object is copied by reference, otherwise it is copied by value.
// Note that an object that is copied by reference may still have its access type modified.
// This can happen when the access is pinned at the point of the component construction.
//
//   - A typical example of "by value" copying is an image that should be stored in the component version,
//     and not in the original registry.
//   - A typical example of "by reference" copying is an image that is already stored in the correct registry
//     and should be referenced by the component version.
type CopyPolicy string

const (
	// CopyPolicyByReference defines that the resource is copied by reference. See CopyPolicy for details.
	CopyPolicyByReference CopyPolicy = "byReference"
	// CopyPolicyByValue defines that the resource is copied by value. See CopyPolicy for details.
	CopyPolicyByValue CopyPolicy = "byValue"
)

// A Source is an artifact which describes the sources that were used to generate one or more of the resources.
// Source elements do not have specific additional formal attributes.
//
// In a component constructor, the Source is defined with an AccessOrInput that specifies if
//
//   - the resource is a resource that is just externally referenced (Access)
//   - the resource is a resource that is embedded / added during component constructor (Input)
//
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/02-elements-toplevel.md#sources
// +k8s:deepcopy-gen=true
type Source struct {
	ElementMeta `json:",inline"`
	Type        string `json:"type"`

	AccessOrInput `json:",inline"`

	ConstructorAttributes `json:",inline"`
}

// AccessOrInput describes the access or input information of a resource or source.
// In a component constructor, there is only one access or input information.
// +k8s:deepcopy-gen=true
type AccessOrInput struct {
	Access *runtime.Raw `json:"access,omitempty"`
	Input  *runtime.Raw `json:"input,omitempty"`
}

func (a *AccessOrInput) HasInput() bool {
	return a.Input != nil
}

func (a *AccessOrInput) HasAccess() bool {
	return a.Access != nil
}

func (a *AccessOrInput) Validate() error {
	if !a.HasInput() && !a.HasAccess() {
		return fmt.Errorf("either access or input must be set")
	}
	if a.HasInput() && a.HasAccess() {
		return fmt.Errorf("only one of access or input must be set, but both are present")
	}
	return nil
}

// Reference describes the reference to another component in the registry.
// A component version may refer to other component versions by adding a reference to the component version.
//
// The Open Component Model makes no assumptions about how content described by the model is finally deployed or used.
// This is left to external tools.
//
// Tool specific deployment information is formally represented by other artifacts with an appropriate type.
//
// In addition to the common artifact information, a resource may optionally describe a reference to the source by specifying its artifact identity.
//
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/02-processing/01-references.md#referencing
// +k8s:deepcopy-gen=true
type Reference struct {
	ElementMeta `json:",inline"`
	// Component describes the remote name of the referenced object.
	Component string `json:"componentName"`
}

// SourceRef defines a reference to a source.
// +k8s:deepcopy-gen=true
type SourceRef struct {
	// IdentitySelector provides selection means for sources.
	IdentitySelector map[string]string `json:"identitySelector,omitempty"`
	// Labels provided for further identification and extra selection rules.
	Labels []Label `json:"labels,omitempty"`
}

// Meta defines the metadata of the component descriptor.
// +k8s:deepcopy-gen=true
type Meta struct {
	// Version is the schema version of the component descriptor.
	Version string `json:"schemaVersion"`
}

// ObjectMeta defines an object that is uniquely identified by its name and version.
// Additionally the object can be defined by an optional set of labels.
// It is an implementation of the Element Identity as per
// https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#element-identity
// +k8s:deepcopy-gen=true
type ObjectMeta struct {
	// Name is the context unique name of the object.
	Name string `json:"name"`
	// Version is the semver version of the object.
	Version string `json:"version"`
	// Labels defines an optional set of additional labels
	// describing the object.
	// +optional
	Labels []Label `json:"labels,omitempty"`
}

func (m *ObjectMeta) String() string {
	base := m.Name
	if m.Version != "" {
		base += ":" + m.Version
	}
	if len(m.Labels) > 0 {
		base += fmt.Sprintf("+labels(%v)", m.Labels)
	}
	return base
}

// ElementMeta defines an object with name and version containing labels.
// +k8s:deepcopy-gen=true
type ElementMeta struct {
	ObjectMeta `json:",inline"`
	// ExtraIdentity is the identity of an object.
	// An additional label with key "name" is not allowed
	ExtraIdentity runtime.Identity `json:"extraIdentity,omitempty"`
}

func (m *ElementMeta) String() string {
	base := m.ObjectMeta.String()
	if m.ExtraIdentity != nil {
		base += fmt.Sprintf("+extraIdentity(%v)", m.ExtraIdentity)
	}
	return base
}

// ToIdentity returns the runtime.Identity equivalent of the ElementMeta.
// It is used to create a unique identity for the object.
func (m *ElementMeta) ToIdentity() runtime.Identity {
	if m == nil {
		return nil
	}
	mp := maps.Clone(m.ExtraIdentity)
	if mp == nil {
		mp = make(runtime.Identity, 2)
	}
	if m.Name != "" {
		mp[IdentityAttributeName] = m.Name
	}
	if m.Version != "" {
		mp[IdentityAttributeVersion] = m.Version
	}
	return mp
}

// ComponentMeta defines an object with name and version containing labels.
// The ObjectMeta is used to contain the Component Identity as per
// https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/02-elements-toplevel.md#component-identity
// +k8s:deepcopy-gen=true
type ComponentMeta struct {
	ObjectMeta `json:",inline"`
	// CreationTime is the creation time of the component version
	CreationTime string `json:"creationTime,omitempty"`
}

// ToIdentity returns the runtime.Identity equivalent of the ElementMeta.
// It is used to create a unique identity for the object.
func (r *ComponentMeta) ToIdentity() runtime.Identity {
	if r == nil {
		return nil
	}
	m := make(runtime.Identity, 2)
	if r.Name != "" {
		m[IdentityAttributeName] = r.Name
	}
	if r.Version != "" {
		m[IdentityAttributeVersion] = r.Version
	}
	return m
}

// Digest defines digest information such as hashing algorithm, normalization and the actual value.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#digest-info
// +k8s:deepcopy-gen=true
type Digest struct {
	HashAlgorithm          string `json:"hashAlgorithm"`
	NormalisationAlgorithm string `json:"normalisationAlgorithm"`
	Value                  string `json:"value"`
}

// Signature contains a list of signatures for the component.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#signatures
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/02-processing/02-signing.md
// +k8s:deepcopy-gen=true
type Signature struct {
	Name      string        `json:"name"`
	Digest    Digest        `json:"digest"`
	Signature SignatureInfo `json:"signature"`
}

// SignatureInfo defines details of a signature.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#signature-info
// +k8s:deepcopy-gen=true
type SignatureInfo struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
	MediaType string `json:"mediaType"`
	Issuer    string `json:"issuer,omitempty"`
}

// Label that can be set on various objects in the Open Component Model domain.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#labels
// +k8s:deepcopy-gen=true
type Label struct {
	// Name is the unique name of the label.
	Name string `json:"name"`
	// Value is the json/yaml data of the label
	Value json.RawMessage `json:"value"`
	// Signing describes whether the label should be included into the signature
	Signing bool `json:"signing,omitempty"`
	// Version is the optional specification version of the attribute value
	Version string `json:"version,omitempty"`
}

// String is a custom string representation of the Label that takes into account the raw string value of the label
// as well as whether it is signing relevant.
func (l Label) String() string {
	base := "label{" + l.Name
	if len(l.Value) > 0 {
		base += fmt.Sprintf("=%s", string(l.Value))
	}
	if l.Signing {
		base += "(signing-relevant)"
	}
	base += "}"
	return base
}

// GetValue returns the label value with the given name as parsed object.
func (in *Label) GetValue(dest interface{}) error {
	return yaml.Unmarshal(in.Value, dest)
}

// SetValue sets the label value by marshalling the given object.
// A passed byte slice is validated to be valid json.
func (in *Label) SetValue(value interface{}) error {
	msg, err := AsRawMessage(value)
	if err != nil {
		return err
	}
	in.Value = msg
	return nil
}

// MustAsRawMessage converts any given value to a json.RawMessage.
// It panics if the conversion fails, so it should only be used when the conversion is guaranteed to succeed.
func MustAsRawMessage(value interface{}) json.RawMessage {
	msg, err := AsRawMessage(value)
	if err != nil {
		panic(fmt.Sprintf("cannot convert value %T to json.RawMessage: %v", value, err))
	}
	return msg
}

// AsRawMessage converts any given value to a json.RawMessage.
func AsRawMessage(value interface{}) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	var (
		data []byte
		ok   bool
		err  error
	)

	if data, ok = value.([]byte); ok {
		var v interface{}
		err = yaml.Unmarshal(data, &v)
		if err != nil {
			return nil, fmt.Errorf("data cannot be encoded as raw message: %s", string(data))
		}
	} else {
		data, err = yaml.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("data of type %T cannot be encoded as raw message", value)
		}
	}
	return bytes.TrimSpace(data), nil
}
