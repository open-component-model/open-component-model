package descriptor

import (
	"fmt"
	"maps"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// cli needs sample of transfer (source repo - target repo + parsing)
// once the command starts up, load plugins, get capabilities of plugins, match repository type from the command
// download component version -> hands me descriptor
// use descriptor to parse, look for the target plugin and use transfer component version
//

type Descriptor struct {
	Meta       Meta        `json:"meta"`
	Component  Component   `json:"component"`
	Signatures []Signature `json:"signatures,omitempty"`
}

func (d *Descriptor) String() string {
	base := d.Component.GetType().String()
	if d.Meta.Version != "" {
		base += fmt.Sprintf(" (schema version %s)", d.Meta.Version)
	}
	return base
}

type Component struct {
	runtime.Identity   `json:",inline"`
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
	Access        runtime.Typed     `json:"access"`
	Digest        *Digest           `json:"digest,omitempty"`
	Size          int64             `json:"size,omitempty"`
}

func (r *Resource) GetIdentity() map[string]string {
	m := maps.Clone(r.ExtraIdentity)
	if m == nil {
		m = make(map[string]string)
	}
	m["name"] = r.GetType().GetName()
	return m
}

type Source struct {
	ObjectMeta    `json:",inline"`
	ExtraIdentity map[string]string `json:"extraIdentity,omitempty"`
	Type          string            `json:"type"`
	Access        runtime.Typed     `json:"access"`
}

func (r *Source) GetIdentity() map[string]string {
	m := maps.Clone(r.ExtraIdentity)
	if m == nil {
		m = make(map[string]string)
	}
	m["name"] = r.GetType().GetName()
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
	runtime.Identity `json:",inline"`
	Labels           []Label `json:"labels,omitempty"`
}

func (o *ObjectMeta) String() string {
	base := o.GetType().GetName()
	if o.GetType().GetVersion() != "" {
		base += ":" + o.GetType().GetVersion()
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
}
