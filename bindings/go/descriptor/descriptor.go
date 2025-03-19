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

type Descriptor struct {
	Meta       Meta        `json:"meta"`
	Component  Component   `json:"component"`
	Signatures []Signature `json:"signatures,omitempty"`
}

func (d *Descriptor) String() string {
	base := d.Component.String()
	if d.Meta.Version != "" {
		base += fmt.Sprintf(" (schema version %s)", d.Meta.Version)
	}
	return base
}

type Component struct {
	ObjectMeta         `json:",inline"`
	Labels             []Label                `json:"labels,omitempty"`
	RepositoryContexts []runtime.Unstructured `json:"repositoryContexts,omitempty"`
	Provider           string                 `json:"provider"`
	Resources          []Resource             `json:"resources,omitempty"`
	Sources            []Source               `json:"sources,omitempty"`
	References         []Reference            `json:"componentReferences,omitempty"`
}

type Resource struct {
	ObjectMeta    `json:",inline"`
	ExtraIdentity map[string]string `json:"extraIdentity,omitempty"`
	SourceRefs    []SourceRef       `json:"sourceRefs,omitempty"`
	Type          string            `json:"type"`
	Relation      string            `json:"relation"`
	Access        runtime.Raw       `json:"access"`
	Digest        *Digest           `json:"digest,omitempty"`
	Size          int64             `json:"size,omitempty"`
	CreationTime  string            `json:"creationTime,omitempty"`
}

func (r *Resource) GetIdentity() map[string]string {
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

func (r *Source) GetIdentity() map[string]string {
	m := maps.Clone(r.ExtraIdentity)
	if m == nil {
		m = make(map[string]string)
	}
	m["name"] = r.Name
	return m
}

type Reference struct {
	Name          string            `json:"name"`
	ExtraIdentity map[string]string `json:"extraIdentity,omitempty"`
	Component     string            `json:"componentName"`
	Version       string            `json:"version"`
	Digest        Digest            `json:"digest,omitempty"`
	Labels        []Label           `json:"labels,omitempty"`
}

type SourceRef struct {
	IdentitySelector map[string]string `json:"identitySelector,omitempty"`
	Labels           []Label           `json:"labels,omitempty"`
}

type Meta struct {
	Version string `json:"schemaVersion"`
}

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

type Digest struct {
	HashAlgorithm          string `json:"hashAlgorithm"`
	NormalisationAlgorithm string `json:"normalisationAlgorithm"`
	Value                  string `json:"value"`
}

type Signature struct {
	Name      string        `json:"name"`
	Digest    Digest        `json:"digest"`
	Signature SignatureSpec `json:"signature"`
}

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
