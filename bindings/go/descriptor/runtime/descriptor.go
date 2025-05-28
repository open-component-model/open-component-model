package runtime

import (
	"fmt"
	"maps"
	"time"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ExcludeFromSignature used in digest field for normalisationAlgorithm (in combination with NoDigest for hashAlgorithm and value)
	// to indicate the resource content should not be part of the signature.
	ExcludeFromSignature = "EXCLUDE-FROM-SIGNATURE"
	// NoDigest used in digest field for hashAlgorithm and value (in combination with ExcludeFromSignature for normalisationAlgorithm)
	// to indicate the resource content should not be part of the signature.
	NoDigest = "NO-DIGEST"
)

// These constants describe identity attributes predefined by the
// model used to identify elements (resources, sources and references)
// in a component version.
const (
	IdentityAttributeName    = "name"
	IdentityAttributeVersion = "version"
)

// Descriptor defines a schema specific descriptor of a component containing additionally embedded signatures
// verifying the validity of the component.
type Descriptor struct {
	// Meta defines the schema version of the component.
	Meta Meta `json:"-"`
	// Component defines the component spec.
	Component Component `json:"-"`
	// Signatures contains optional signing information.
	Signatures []Signature `json:"-"`
}

func (d *Descriptor) String() string {
	base := d.Component.String()
	if d.Meta.Version != "" {
		base += fmt.Sprintf(" (schema version %s)", d.Meta.Version)
	}
	return base
}

// Component defines a named and versioned component containing dependencies such as sources, resources and
// references pointing to further component versions.
type Component struct {
	ComponentMeta `json:",inline"`
	// RepositoryContexts defines the previous repositories of the component.
	// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#repository-contexts
	RepositoryContexts []runtime.Typed `json:"-"`
	// Provider describes the provider type of component in the origin's context.
	Provider Provider `json:"-"`
	// Resources defines all resources that are created by the component and by a third party.
	Resources []Resource `json:"-"`
	// Sources defines sources that produced the component.
	Sources []Source `json:"-"`
	// References component dependencies that can be resolved in the current context.
	References []Reference `json:"-"`
}

// Provider describes the provider of the component.
type Provider struct {
	Name   string  `json:"-"`
	Labels []Label `json:"-"`
}

// ResourceRelation describes whether the component is created by a third party or internally.
type ResourceRelation string

const (
	// LocalRelation defines a internal relation
	// which describes an internally maintained resource in the origin's context.
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
	// SourceRefs defines a list of sources used to generate the resource.
	// These entries reference the sources defined in the
	// component.sources.
	SourceRefs []SourceRef `json:"-"`
	// Type describes the type of the resource.
	Type string `json:"-"`
	// Relation describes the relation of the resource to the component.
	// Can be a local or external resource.
	Relation ResourceRelation `json:"-"`
	// Access defines the type of access this resource requires.
	Access runtime.Typed `json:"-"`
	// Digest is the optional digest of the referenced resource.
	Digest *Digest `json:"-"`
	// Size of the resource blob.
	Size int64 `json:"-"`
	// CreationTime of the resource.
	CreationTime CreationTime `json:"-"`
}

type CreationTime time.Time

func (t CreationTime) DeepCopyInto(to *CreationTime) {
	*to = t
}

// A Source is an artifact which describes the sources that were used to generate one or more of the resources.
// Source elements do not have specific additional formal attributes.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/02-elements-toplevel.md#sources
// +k8s:deepcopy-gen=true
type Source struct {
	ElementMeta `json:",inline"`
	Type        string        `json:"-"`
	Access      runtime.Typed `json:"-"`
}

// Reference describes the reference to another component.
// A component version may refer to other component versions by adding a reference to the component version.
//
// The Open Component Model makes no assumptions about how content described by the model is finally deployed or used.
// This is left to external tools.
//
// Tool specific deployment information is formally represented by other artifacts with an appropriate type and/or by labels.
//
// In addition to the common artifact information, a resource may optionally describe a reference to the source by specifying its artifact identity.
//
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/02-processing/01-references.md#referencing
// +k8s:deepcopy-gen=true
type Reference struct {
	// The name in ElementMeta describes the name of the reference itself within this component (not the name of the referenced component). But the version in ElementMeta specifies the version of the referenced component.
	ElementMeta `json:",inline"`
	// Component describes the remote name of the referenced object.
	Component string `json:"-"`
	// Digest of the reference.
	Digest Digest `json:"-"`
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
	// +optional
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
// +k8s:deepcopy-gen=true
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
	mp[IdentityAttributeName] = m.Name
	mp[IdentityAttributeVersion] = m.Version
	return mp
}

// ComponentMeta defines an object with name and version containing labels.
// The ObjectMeta is used to contain the Component Identity as per
// https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/02-elements-toplevel.md#component-identity
// +k8s:deepcopy-gen=true
type ComponentMeta struct {
	ObjectMeta `json:",inline"`
	// CreationTime is the creation time of the component version
	CreationTime string `json:"-"`
}

// ToIdentity returns the runtime.Identity equivalent of the ElementMeta.
// It is used to create a unique identity for the object.
func (r *ComponentMeta) ToIdentity() runtime.Identity {
	if r == nil {
		return nil
	}
	m := make(runtime.Identity, 2)
	m[IdentityAttributeName] = r.Name
	m[IdentityAttributeVersion] = r.Version
	return m
}

// Digest defines digest information such as hashing algorithm, normalization and the actual value.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#digest-info
// +k8s:deepcopy-gen=true
type Digest struct {
	HashAlgorithm          string `json:"-"`
	NormalisationAlgorithm string `json:"-"`
	Value                  string `json:"-"`
}

// Signature contains a list of signatures for the component.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#signatures
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/02-processing/02-signing.md
// +k8s:deepcopy-gen=true
type Signature struct {
	Name      string        `json:"-"`
	Digest    Digest        `json:"-"`
	Signature SignatureInfo `json:"-"`
}

// SignatureInfo defines details of a signature.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#signature-info
// +k8s:deepcopy-gen=true
type SignatureInfo struct {
	Algorithm string `json:"-"`
	Value     string `json:"-"`
	MediaType string `json:"-"`
	Issuer    string `json:"-"`
}

// Label that can be set on various objects in the Open Component Model domain.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#labels
// +k8s:deepcopy-gen=true
type Label struct {
	Name  string `json:"-"`
	Value string `json:"-"`
	// Signing describes whether the label should be included into the signature
	Signing bool `json:"-"`
}

// NewExcludeFromSignatureDigest returns the special digest notation to indicate the resource content should not be part of the signature.
func NewExcludeFromSignatureDigest() *Digest {
	return &Digest{
		HashAlgorithm:          NoDigest,
		NormalisationAlgorithm: ExcludeFromSignature,
		Value:                  NoDigest,
	}
}
