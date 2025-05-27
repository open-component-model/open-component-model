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
	// Provider describes the provider type of component in the origin's context.
	Provider Provider `json:"-"`
	// Resources defines all resources that are created by the component and by a third party.
	Resources []Resource `json:"-"`
	// Sources defines sources that produced the component.
	Sources []Source `json:"-"`
	// References component dependencies that can be resolved in the current context.
	References []Reference `json:"-"`
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
// It is an implementation of the Element Identity as per
// https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#element-identity
type ElementMeta struct {
	ObjectMeta `json:",inline"`
	// ExtraIdentity is the identity of an object.
	// An additional identity attribute with key "name" is not allowed
	ExtraIdentity runtime.Identity `json:"-"`
}

func (m *ElementMeta) String() string {
	base := m.ObjectMeta.String()
	if m.ExtraIdentity != nil {
		base += fmt.Sprintf("+extraIdentity(%v)", m.ExtraIdentity)
	}
	return base
}

func (m *ElementMeta) ToIdentity() runtime.Identity {
	if m.ExtraIdentity == nil {
		return runtime.Identity{
			IdentityAttributeName:    m.Name,
			IdentityAttributeVersion: m.Version,
		}
	}
	return maps.Clone(m.ExtraIdentity)
}

// ComponentMeta defines the metadata of a component.
type ComponentMeta struct {
	ObjectMeta `json:",inline"`
	// CreationTime is the creation time of the component version
	CreationTime string `json:"-"`
}

func (r *ComponentMeta) ToIdentity() runtime.Identity {
	return runtime.Identity{
		IdentityAttributeName:    r.Name,
		IdentityAttributeVersion: r.Version,
	}
}

// Label defines a label that can be used to add additional information to an element.
type Label struct {
	Name  string `json:"-"`
	Value string `json:"-"`
	// Signing describes whether the label should be included into the signature
	Signing bool `json:"-"`
}

// Validate checks for duplicate identities and validates AccessOrInput fields in the component constructor.
func (cc *ComponentConstructor) Validate() error {
	if cc == nil {
		return nil
	}
	// Track identities for resources, sources, and references
	resourceIdentities := make(map[string]int)
	sourceIdentities := make(map[string]int)
	referenceIdentities := make(map[string]int)

	for ci, c := range cc.Components {
		for ri, r := range c.Resources {
			id := r.ElementMeta.ToIdentity()
			key := fmt.Sprintf("resource:%v", id)
			if _, exists := resourceIdentities[key]; exists {
				return fmt.Errorf("duplicate resource identity in component %d, resource %d: %v", ci, ri, id)
			}
			resourceIdentities[key] = 1
			if err := r.AccessOrInput.Validate(); err != nil {
				return fmt.Errorf("invalid AccessOrInput in component %d, resource %d: %w", ci, ri, err)
			}
		}
		for si, s := range c.Sources {
			id := s.ElementMeta.ToIdentity()
			key := fmt.Sprintf("source:%v", id)
			if _, exists := sourceIdentities[key]; exists {
				return fmt.Errorf("duplicate source identity in component %d, source %d: %v", ci, si, id)
			}
			sourceIdentities[key] = 1
			if err := s.AccessOrInput.Validate(); err != nil {
				return fmt.Errorf("invalid AccessOrInput in component %d, source %d: %w", ci, si, err)
			}
		}
		for ri, r := range c.References {
			id := r.ElementMeta.ToIdentity()
			key := fmt.Sprintf("reference:%v", id)
			if _, exists := referenceIdentities[key]; exists {
				return fmt.Errorf("duplicate reference identity in component %d, reference %d: %v", ci, ri, id)
			}
			referenceIdentities[key] = 1
		}
	}
	return nil
}
