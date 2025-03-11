package descriptor

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"strings"

	v1 "ocm.software/open-component-model/bindings/golang/config/v1"
	"ocm.software/open-component-model/bindings/golang/descriptor/codec"
	"ocm.software/open-component-model/bindings/golang/runtime"
)

// cli needs sample of transfer (source repo - target repo + parsing)
// once the command starts up, load plugins, get capabilities of plugins, match repository type from the command
// download component version -> hands me descriptor
// use descriptor to parse, look for the target plugin and use transfer component version
//

type Descriptor struct {
	Meta       *Meta        `json:"meta"`
	Component  *Component   `json:"component"`
	Signatures []*Signature `json:"signatures,omitempty"`
}

func (d *Descriptor) String() string {
	base := fmt.Sprintf("%s", d.Component)
	if d.Meta.Version != "" {
		base += fmt.Sprintf(" (schema version %s)", d.Meta.Version)
	}
	return base
}

type EncodeOptions struct {
	SchemaVersion string
}

type EncodeOption func(*EncodeOptions)

func (d *Descriptor) Encode(writer io.Writer, encoder func(writer io.Writer) (codec.Encoder, error), opts ...EncodeOption) error {
	enc, err := encoder(writer)
	if err != nil {
		return fmt.Errorf("could not create encoder: %w", err)
	}
	err = enc.Encode(d)
	if err != nil {
		return fmt.Errorf("could not encode descriptor: %w", err)
	}
	return nil
}

type DecodeOptions struct{}

type DecodeOption func(*DecodeOptions)

func Decode(reader io.Reader, decoder func(reader io.Reader) (codec.Decoder, error), opts ...DecodeOption) (*Descriptor, error) {
	var d Descriptor
	dec, err := decoder(reader)
	if err != nil {
		return nil, fmt.Errorf("could not create decoder: %w", err)
	}
	err = dec.Decode(&d)
	if err != nil {
		return nil, fmt.Errorf("could not decode descriptor: %w", err)
	}
	return &d, nil
}

type Component struct {
	ComponentIdentity  `json:",inline"`
	Labels             []*Label                `json:"labels,omitempty"`
	RepositoryContexts []*runtime.Unstructured `json:"repositoryContexts,omitempty"`
	Provider           string                  `json:"provider"`
	Resources          []*Resource             `json:"resources,omitempty"`
	Sources            []*Source               `json:"sources,omitempty"`
	References         []*Reference            `json:"componentReferences,omitempty"`
}

type ComponentIdentity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (c *ComponentIdentity) String() string {
	return fmt.Sprintf("%s:%s", c.Name, c.Version)
}

func ParseComponentIdentity(s string) (ComponentIdentity, error) {
	ci := ComponentIdentity{}
	split := strings.Split(s, ":")
	if len(split) != 2 {
		return ci, fmt.Errorf("invalid component identity %q", s)
	}
	ci.Name = split[0]
	ci.Version = split[1]
	if ci.Name == "" || ci.Version == "" {
		return ci, fmt.Errorf("invalid component identity %q", s)
	}
	return ci, nil
}

func (c *ComponentIdentity) AsIdentity() v1.Identity {
	return v1.Identity{
		"name":    c.Name,
		"version": c.Version,
	}
}

type Resource struct {
	ObjectMeta    `json:",inline"`
	ExtraIdentity map[string]string `json:"extraIdentity,omitempty"`
	SourceRefs    []*SourceRef      `json:"sourceRefs,omitempty"`
	Type          string            `json:"type"`
	Relation      string            `json:"relation"`
	Access        Access            `json:"access"`
	Digest        *Digest           `json:"digest,omitempty"`
	Size          int64             `json:"size,omitempty"`
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
	Access        Access            `json:"access"`
}

func (r *Source) GetIdentity() map[string]string {
	m := maps.Clone(r.ExtraIdentity)
	if m == nil {
		m = make(map[string]string)
	}
	m["name"] = r.Name
	return m
}

type Access struct {
	runtime.Typed `json:",inline"`
}

func (a *Access) UnmarshalJSON(data []byte) error {
	a.Typed = &runtime.Raw{}
	return json.Unmarshal(data, a.Typed)
}

func (a *Access) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.Typed)
}

type Reference struct {
	Name          string            `json:"name"`
	ExtraIdentity map[string]string `json:"extraIdentity,omitempty"`
	Component     string            `json:"componentName"`
	Version       string            `json:"version"`
	Digest        Digest            `json:"digest,omitempty"`
	Labels        []*Label          `json:"labels,omitempty"`
}

type SourceRef struct {
	IdentitySelector map[string]string `json:"identitySelector,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

type Meta struct {
	Version string `json:"schemaVersion"`
}

type ObjectMeta struct {
	ComponentIdentity `json:",inline"`
	Labels            []*Label `json:"labels,omitempty"`
}

func (o *ObjectMeta) String() string {
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
}
