package runtime

import (
	"fmt"
	"maps"

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
type ComponentConstructor struct {
	Components []Component `json:"-"`
}

// Component defines a named and versioned component containing dependencies such as sources, resources and
// references pointing to further component versions.
type Component struct {
	ComponentMeta `json:",inline"`
	Provider      Provider    `json:"-"`
	Resources     []Resource  `json:"-"`
	Sources       []Source    `json:"-"`
	References    []Reference `json:"-"`
}

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
}

// A Source is an artifact which describes the sources that were used to generate one or more of the resources.
// Source elements do not have specific additional formal attributes.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/02-elements-toplevel.md#sources
type Source struct {
	ElementMeta `json:",inline"`
	Type        string `json:"-"`
	// AccessOrInput defines the access or input information of the source.
	// In a component constructor, there is only one access or input information.
	AccessOrInput `json:"-"`
}

// AccessOrInput describes the access or input information of a resource or source.
// In a component constructor, there is only one access or input information.
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
type Reference struct {
	ElementMeta `json:",inline"`
	// Component describes the remote name of the referenced object.
	Component string `json:"-"`
}

// SourceRef defines a reference to a source.
type SourceRef struct {
	// IdentitySelector provides selection means for sources.
	IdentitySelector map[string]string `json:"-"`
	// Labels provided for further identification and extra selection rules.
	Labels []Label `json:"-"`
}

// Meta defines the metadata of the component descriptor.
type Meta struct {
	// Version is the schema version of the component descriptor.
	Version string `json:"-"`
}

// ObjectMeta defines an object that is uniquely identified by its name and version.
// Additionally the object can be defined by an optional set of labels.
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
	mp[IdentityAttributeName] = m.Name
	mp[IdentityAttributeVersion] = m.Version
	return mp
}

// ComponentMeta defines the metadata of a component.
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
	m[IdentityAttributeName] = r.Name
	m[IdentityAttributeVersion] = r.Version
	return m
}

// Label defines a label that can be used to add additional information to an element.
type Label struct {
	Name  string `json:"-"`
	Value string `json:"-"`
	// Signing describes whether the label should be included into the signature
	Signing bool `json:"-"`
}
