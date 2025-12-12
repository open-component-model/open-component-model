package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"

	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// These constants describe identity attributes predefined by the
// model used to identify elements (resources, sources and references)
// in a component version.
const (
	IdentityAttributeName    = "name"
	IdentityAttributeVersion = "version"
)

// ComponentConstructor defines a constructor for creating component versions.
// +k8s:deepcopy-gen=true
type ComponentConstructor struct {
	Components []Component `json:"-"`
}

// Component defines a named and versioned component containing dependencies such as sources, resources and
// references pointing to further component versions.
// +k8s:deepcopy-gen=true
type Component struct {
	ComponentMeta `json:",inline"`
	Provider      Provider    `json:"-"`
	Resources     []Resource  `json:"-"`
	Sources       []Source    `json:"-"`
	References    []Reference `json:"-"`
}

// Provider describes the provider of a component.
// It contains a name and optional labels.
// +k8s:deepcopy-gen=true
type Provider struct {
	Name   string  `json:"-"`
	Labels []Label `json:"-"`
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
// For example, installation procedures or meta-model descriptions controlling orchestration and/or deployment mechanisms.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/02-elements-toplevel.md#resources
// +k8s:deepcopy-gen=true
type Resource struct {
	ElementMeta `json:",inline"`
	// SourceRefs defines a list of source names.
	// These entries reference the sources defined in the
	// component.sources.
	SourceRefs []SourceRef `json:"-"`
	// Type describes the type of the object.
	Type string `json:"-"`
	// Relation describes the relation of the resource to the component.
	// Can be a local or external resource.
	Relation ResourceRelation `json:"-"`
	// AccessOrInput defines the access or input information of the resource.
	// In a component constructor, there is only one access or input information.
	AccessOrInput `json:"-"`

	ConstructorAttributes `json:"-"`
}

// ConstructorAttributes defines additional attributes used during component construction.
// +k8s:deepcopy-gen=true
type ConstructorAttributes struct {
	CopyPolicy `json:"-"`
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

// Source is an artifact which describes the sources that were used to generate one or more of the resources.
// Source elements do not have specific additional formal attributes.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/02-elements-toplevel.md#sources
// +k8s:deepcopy-gen=true
type Source struct {
	ElementMeta `json:",inline"`
	Type        string `json:"-"`
	// AccessOrInput defines the access or input information of the source.
	// In a component constructor, there is only one access or input information.
	AccessOrInput `json:"-"`

	ConstructorAttributes `json:"-"`
}

// AccessOrInput describes the access or input information of a resource or source.
// In a component constructor, there is only one access or input information.
// +k8s:deepcopy-gen=true
type AccessOrInput struct {
	Access runtime.Typed `json:"-"`
	Input  runtime.Typed `json:"-"`
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
// +k8s:deepcopy-gen=true
type Reference struct {
	ElementMeta `json:",inline"`
	// Component describes the remote name of the referenced object.
	Component string `json:"-"`
}

func (r *Reference) ToComponentIdentity() runtime.Identity {
	if r == nil {
		return nil
	}
	m := make(runtime.Identity, 2)
	if r.Component != "" {
		m[IdentityAttributeName] = r.Component
	}
	if r.Version != "" {
		m[IdentityAttributeVersion] = r.Version
	}
	return m
}

// SourceRef defines a reference to a source.
// +k8s:deepcopy-gen=true
type SourceRef struct {
	// IdentitySelector provides selection means for sources.
	IdentitySelector map[string]string `json:"-"`
	// Labels provided for further identification and extra selection rules.
	Labels []Label `json:"-"`
}

// Meta defines the metadata of the component descriptor.
// +k8s:deepcopy-gen=true
type Meta struct {
	// Version is the schema version of the component descriptor.
	Version string `json:"-"`
}

// ObjectMeta defines an object that is uniquely identified by its name and version.
// Additionally the object can be defined by an optional set of labels.
// +k8s:deepcopy-gen=true
type ObjectMeta struct {
	// Name is the context unique name of the object.
	Name string `json:"-"`
	// Version is the semver version of the object.
	Version string `json:"-"`
	// Labels defines an optional set of additional labels
	// describing the object.
	Labels []Label `json:"-"`
}

// ElementMeta defines an object with name and version containing labels.
// It is an implementation of the Element Identity as per
// https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#element-identity
// +k8s:deepcopy-gen=true
type ElementMeta struct {
	ObjectMeta `json:",inline"`
	// ExtraIdentity is the identity of an object.
	// An additional identity attribute with key "name" is not allowed
	ExtraIdentity runtime.Identity `json:"-"`
}

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

// ComponentMeta defines the metadata of a component.
// +k8s:deepcopy-gen=true
type ComponentMeta struct {
	ObjectMeta `json:",inline"`
	// CreationTime is the creation time of the component version
	CreationTime string `json:"-"`
}

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

// Label that can be set on various objects in the Open Component Model domain.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#labels
// +k8s:deepcopy-gen=true
type Label struct {
	// Name is the unique name of the label.
	Name string `json:"-"`
	// Value is the json/yaml data of the label
	Value json.RawMessage `json:"-"`
	// Signing describes whether the label should be included into the signature
	Signing bool `json:"-"`
	// Version is the optional specification version of the attribute value
	Version string `json:"-"`
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
