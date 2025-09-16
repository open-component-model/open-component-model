package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"sigs.k8s.io/yaml"

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
	CreationTime string `json:"-"`
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

// Digest defines the hash-based fingerprint of a component descriptor or artifact.
// It combines the hashing algorithm, normalization procedure, and the resulting value.
// Digests are used as canonical identifiers for verifying integrity.
//
// See specification reference:
//   - https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#digest-info
//
// +k8s:deepcopy-gen=true
type Digest struct {
	// HashAlgorithm specifies the hashing algorithm applied after normalization.
	// The choice of algorithm impacts compatibility across verifiers.
	//
	// See specification reference:
	//   - https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/digest-algorithms.md
	HashAlgorithm string `json:"-"`

	// NormalisationAlgorithm defines how the component descriptor or artifact
	// is transformed into a stable byte representation before hashing.
	// Normalization ensures reproducibility by excluding volatile fields
	// such as transport-related access specifications.
	//
	// See specification references:
	//   - https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/component-descriptor-normalization-algorithms.md
	//   - https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/artifact-normalization-types.md
	NormalisationAlgorithm string `json:"-"`

	// Value is the encoded digest result produced from the normalized representation.
	// Typically hex or base64 encoded, depending on the algorithm specification.
	Value string `json:"-"`
}

// Signature represents a cryptographic attestation of a component version descriptor.
// It binds a digest of the descriptor to a cryptographic signature, proving both
// integrity (descriptor unchanged since signing) and authenticity (signed by a known issuer).
//
// A component version may carry multiple signatures, each using different digest
// algorithms, normalization procedures, or signing keys. Every signature must
// be uniquely identified by its Name.
//
// See specification references:
//   - https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#signatures
//   - https://github.com/open-component-model/ocm-spec/blob/main/doc/02-processing/02-signing.md
//
// +k8s:deepcopy-gen=true
type Signature struct {
	// Name is the unique identifier of the signature within the component version.
	// Enables consumers to explicitly verify or remove a specific signature.
	Name string `json:"-"`

	// Digest is the canonical hash of the signed component descriptor.
	// The digest must be computed using the declared HashAlgorithm and
	// NormalisationAlgorithm. Volatile fields (e.g., access specifications)
	// MUST be excluded to keep signatures reproducible across transports.
	Digest Digest `json:"-"`

	// Signature is the metadata and cryptographic payload proving the authenticity
	// of the digest. It includes details on the algorithm, encoding, and issuer.
	Signature SignatureInfo `json:"-"`
}

// SignatureInfo provides the metadata and cryptographic material for a signature.
// It is used during signature verification to determine how to interpret and validate
// the signature value against the associated digest.
//
// See specification reference:
//   - https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#signature-info
//
// +k8s:deepcopy-gen=true
type SignatureInfo struct {
	// Algorithm specifies the cryptographic signing algorithm.
	// Consumers select the corresponding verification procedure based on this value.
	//
	// See specification reference:
	//   - https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/signing-algorithms.md
	Algorithm string `json:"-"`

	// Value contains the raw cryptographic signature over the digest.
	// Encoding is typically base64 or hex, depending on Algorithm and MediaType.
	//
	// See specification reference:
	//   - https://datatracker.ietf.org/doc/html/rfc4648
	Value string `json:"-"`

	// MediaType describes the technical encoding format of the Value.
	// It provides consumers with the necessary context to decode and interpret the signature.
	MediaType string `json:"-"`

	// Issuer optionally identifies the signer of the signature.
	// Values can be:
	//   - an RFC2253 Distinguished Name (DN) string,
	//   - or a free-form string identifier for the signing authority.
	// If provided, it should be used to match the expected identity of the signer.
	//
	// See RFC 2253 for DN formatting:
	//   - https://datatracker.ietf.org/doc/html/rfc2253
	Issuer string `json:"-"`
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

// NewExcludeFromSignatureDigest returns the special digest notation to indicate the resource content should not be part of the signature.
func NewExcludeFromSignatureDigest() *Digest {
	return &Digest{
		HashAlgorithm:          NoDigest,
		NormalisationAlgorithm: ExcludeFromSignature,
		Value:                  NoDigest,
	}
}
