package descriptor

import (
	"fmt"
	"maps"

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

func (d Descriptor) String() string {
	base := d.Component.String()
	if d.Meta.Version != "" {
		base += fmt.Sprintf(" (schema version %s)", d.Meta.Version)
	}
	return base
}

// Component defines a named and versioned component containing dependencies such as sources, resources and
// references pointing to further component versions.
type Component struct {
	ObjectMeta `json:",inline"`
	// Labels defines an optional set of additional labels
	// describing the object.
	Labels []Label `json:"labels,omitempty"`
	// RepositoryContexts defines the previous repositories of the component.
	RepositoryContexts []runtime.Unstructured `json:"repositoryContexts,omitempty"`
	// Provider described the component provider.
	Provider string `json:"provider"`
	// Resources defines all resources that are created by the component and by a third party.
	Resources []Resource `json:"resources,omitempty"`
	// Sources defines sources that produced the component.
	Sources []Source `json:"sources,omitempty"`
	// References component dependencies that can be resolved in the current context.
	References []Reference `json:"componentReferences,omitempty"`
}

type Resource struct {
	ObjectMeta `json:",inline"`
	// ExtraIdentity is the identity of an object.
	// An additional label with key "name" is not allowed.
	ExtraIdentity map[string]string `json:"extraIdentity,omitempty"`
	// SourceRefs defines a list of source names.
	// These entries reference the sources defined in the
	// component.sources.
	SourceRefs []SourceRef `json:"sourceRefs,omitempty"`
	// Type describes the type of the object.
	Type string `json:"type"`
	// Relation describes the relation of the resource to the component.
	// Can be a local or external resource.
	Relation string `json:"relation"`
	// Access defines the type of access this resource requires.
	Access runtime.Raw `json:"access"`
	// Digest is the optional digest of the referenced resource.
	Digest *Digest `json:"digest,omitempty"`
	// Size of the resource blob.
	Size int64 `json:"size,omitempty"`
	// CreationTime of the resource.
	CreationTime string `json:"creationTime,omitempty"`
}

func (r Resource) GetIdentity() map[string]string {
	m := maps.Clone(r.ExtraIdentity)
	if m == nil {
		m = make(map[string]string)
	}
	m["name"] = r.Name
	return m
}

type Source struct {
	ObjectMeta    `json:",inline"`
	ExtraIdentity map[string]string `json:"extraIdentity,omitempty"`
	Type          string            `json:"type"`
	Access        runtime.Raw       `json:"access"`
}

func (r Source) GetIdentity() map[string]string {
	m := maps.Clone(r.ExtraIdentity)
	if m == nil {
		m = make(map[string]string)
	}
	m["name"] = r.Name
	return m
}

// Reference describes the reference to another component in the registry.
type Reference struct {
	// Name of the reference.
	Name string `json:"name"`
	// ExtraIdentity defines additional information.
	ExtraIdentity map[string]string `json:"extraIdentity,omitempty"`
	// Component describes the remote name of the referenced object.
	Component string `json:"componentName"`
	// Version defines the version of the reference.
	Version string `json:"version"`
	// Digest of the reference.
	Digest Digest `json:"digest,omitempty"`
	// Labels provided for further identification and extra selection rules.
	Labels []Label `json:"labels,omitempty"`
}

// SourceRef defines a reference to a source.
type SourceRef struct {
	// IdentitySelector provides selection means for sources.
	IdentitySelector map[string]string `json:"identitySelector,omitempty"`
	// Labels provided for further identification and extra selection rules.
	Labels []Label `json:"labels,omitempty"`
}

type Meta struct {
	Version string `json:"schemaVersion"`
}

// ObjectMeta defines an object with name and version containing labels.
type ObjectMeta struct {
	Name    string  `json:"name"`
	Version string  `json:"version"`
	Labels  []Label `json:"labels,omitempty"`
}

func (o ObjectMeta) String() string {
	base := o.Name
	if o.Version != "" {
		base += ":" + o.Version
	}
	if o.Labels != nil {
		base += fmt.Sprintf(" (%v)", o.Labels)
	}
	return base
}

// Digest defines digest information such as hashing algorithm, normalization and the actual value.
type Digest struct {
	HashAlgorithm          string `json:"hashAlgorithm"`
	NormalisationAlgorithm string `json:"normalisationAlgorithm"`
	Value                  string `json:"value"`
}

// Signature contains a list of signatures for the component.
type Signature struct {
	Name      string        `json:"name"`
	Digest    Digest        `json:"digest"`
	Signature SignatureSpec `json:"signature"`
}

// SignatureSpec defines details of a signature.
type SignatureSpec struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
	MediaType string `json:"mediaType"`
	Issuer    string `json:"issuer,omitempty"`
}

type Label struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	// Signing describes whether the label should be included into the signature
	Signing bool `json:"signing,omitempty"`
}
