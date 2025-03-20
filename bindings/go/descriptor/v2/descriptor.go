package descriptor

import (
	"errors"
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

// Descriptor defines a schema specific descriptor of a component containing additionally embedded signatures
// verifying the validity of the component.
type Descriptor struct {
	// Meta defines the schema version of the component.
	Meta Meta `json:"meta"`
	// Component defines the component spec.
	Component Component `json:"component"`
	// Signatures contains optional signing information.
	Signatures []Signature `json:"signatures,omitempty"`
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
	RepositoryContexts []runtime.Unstructured `json:"repositoryContexts,omitempty"`
	// Provider describes the provider type of component in the origin's context.
	Provider string `json:"provider"`
	// Resources defines all resources that are created by the component and by a third party.
	Resources []Resource `json:"resources,omitempty"`
	// Sources defines sources that produced the component.
	Sources []Source `json:"sources,omitempty"`
	// References component dependencies that can be resolved in the current context.
	References []Reference `json:"componentReferences,omitempty"`
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
	SourceRefs []SourceRef `json:"sourceRefs,omitempty"`
	// Type describes the type of the object.
	Type string `json:"type"`
	// Relation describes the relation of the resource to the component.
	// Can be a local or external resource.
	Relation ResourceRelation `json:"relation"`
	// Access defines the type of access this resource requires.
	Access runtime.Raw `json:"access"`
	// Digest is the optional digest of the referenced resource.
	Digest *Digest `json:"digest,omitempty"`
	// Size of the resource blob.
	Size int64 `json:"size,omitempty"`
	// CreationTime of the resource.
	CreationTime *Timestamp `json:"creationTime,omitempty"`
}

func (r *Resource) GetIdentity() map[string]string {
	m := maps.Clone(r.ExtraIdentity)
	if m == nil {
		m = make(map[string]string)
	}
	m["name"] = r.Name
	return m
}

// A Source is an artifact which describes the sources that were used to generate one or more of the resources.
// Source elements do not have specific additional formal attributes.
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/02-elements-toplevel.md#sources
// +k8s:deepcopy-gen=true
type Source struct {
	ElementMeta `json:",inline"`
	Type        string      `json:"type"`
	Access      runtime.Raw `json:"access"`
}

func (r *Source) GetIdentity() map[string]string {
	m := maps.Clone(r.ExtraIdentity)
	if m == nil {
		m = make(map[string]string)
	}
	m["name"] = r.Name
	return m
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
	// Digest of the reference.
	Digest Digest `json:"digest,omitempty"`
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

// ObjectMeta defines a object that is uniquely identified by its name and version.
// Additionally the object can be defined by an optional set of labels.
// Its an implementation of the Element Identity as per
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
	if m.Labels != nil {
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

// ComponentMeta defines an object with name and version containing labels.
// The ObjectMeta is used to contain the Component Identity as per
// https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/02-elements-toplevel.md#component-identity
// +k8s:deepcopy-gen=true
type ComponentMeta struct {
	ObjectMeta `json:",inline"`
	// CreationTime is the creation time of the component version
	CreationTime string `json:"creationTime,omitempty"`
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
	Name  string `json:"name"`
	Value string `json:"value"`
	// Signing describes whether the label should be included into the signature
	Signing bool `json:"signing,omitempty"`
}

// NewExcludeFromSignatureDigest returns the special digest notation to indicate the resource content should not be part of the signature.
func NewExcludeFromSignatureDigest() *Digest {
	return &Digest{
		HashAlgorithm:          NoDigest,
		NormalisationAlgorithm: ExcludeFromSignature,
		Value:                  NoDigest,
	}
}

// Timestamp is time rounded to seconds.
// +k8s:deepcopy-gen=true
type Timestamp struct {
	Time `json:",inline"`
}

// MarshalJSON implements the json.Marshaler interface.
// The time is a quoted string in RFC 3339 format, with sub-second precision added if present.
func (t *Timestamp) MarshalJSON() ([]byte, error) {
	if y := t.Year(); y < 0 || y >= 10000 {
		// RFC 3339 is clear that years are 4 digits exactly.
		// See golang.org/issue/4556#c15 for more discussion.
		return nil, errors.New("Time.MarshalJSON: year outside of range [0,9999]")
	}

	b := make([]byte, 0, len(time.RFC3339)+2)
	b = append(b, '"')
	b = t.AppendFormat(b, time.RFC3339)
	b = append(b, '"')
	return b, nil
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// The time is expected to be a quoted string in RFC 3339 format.
func (t *Timestamp) UnmarshalJSON(data []byte) error {
	// Ignore null, like in the main JSON package.
	if string(data) == "null" {
		return nil
	}

	// Fractional seconds are handled implicitly by Parse.
	tt, err := time.Parse(`"`+time.RFC3339+`"`, string(data))
	if err != nil {
		return err
	}
	*t = Timestamp{
		NewTime(tt.UTC().Round(time.Second)),
	}
	return err
}
