package runtime

import (
	"encoding/json"
	"fmt"
	"maps"
	"time"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ConvertFromV2 converts a v2.Descriptor to a custom Descriptor.
func ConvertFromV2(descriptor *v2.Descriptor) (*Descriptor, error) {
	provider, err := ConvertFromV2Provider(descriptor.Component.Provider)
	if err != nil {
		return nil, err
	}
	return &Descriptor{
		Meta: Meta{
			descriptor.Meta.Version,
		},
		Component: Component{
			ComponentMeta: ComponentMeta{
				ObjectMeta: ObjectMeta{
					Name:    descriptor.Component.Name,
					Version: descriptor.Component.Version,
					Labels:  ConvertFromV2Labels(descriptor.Component.Labels),
				},
				CreationTime: descriptor.Component.CreationTime,
			},
			RepositoryContexts: ConvertFromV2RepositoryContexts(descriptor.Component.RepositoryContexts),
			Provider:           provider,
			Resources:          ConvertFromV2Resources(descriptor.Component.Resources),
			Sources:            ConvertFromV2Sources(descriptor.Component.Sources),
			References:         ConvertFromV2References(descriptor.Component.References),
		},
		Signatures: ConvertFromV2Signatures(descriptor.Signatures),
	}, nil
}

// ConvertToV2 converts a custom Descriptor back to a v2.Descriptor.
func ConvertToV2(descriptor *Descriptor) (*v2.Descriptor, error) {
	provider, err := ConvertToV2Provider(descriptor.Component.Provider)
	if err != nil {
		return nil, err
	}
	return &v2.Descriptor{
		Meta: v2.Meta{
			Version: descriptor.Meta.Version,
		},
		Component: v2.Component{
			ComponentMeta: v2.ComponentMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    descriptor.Component.Name,
					Version: descriptor.Component.Version,
					Labels:  ConvertToV2Labels(descriptor.Component.Labels),
				},
				CreationTime: descriptor.Component.CreationTime,
			},
			RepositoryContexts: ConvertToV2RepositoryContexts(descriptor.Component.RepositoryContexts),
			Provider:           provider,
			Resources:          ConvertToV2Resources(descriptor.Component.Resources),
			Sources:            ConvertToV2Sources(descriptor.Component.Sources),
			References:         ConvertToV2References(descriptor.Component.References),
		},
		Signatures: ConvertToV2Signatures(descriptor.Signatures),
	}, nil
}

// ConvertFromV2Provider converts a provider string to a runtime.Identity.
func ConvertFromV2Provider(provider string) (runtime.Identity, error) {
	if json.Valid([]byte(provider)) {
		var id runtime.Identity
		if err := json.Unmarshal([]byte(provider), &id); err != nil {
			return nil, fmt.Errorf("could not unmarshal provider string: %w", err)
		}
		return id, nil
	}
	return runtime.Identity{
		v2.IdentityAttributeName: provider,
	}, nil
}

// ConvertFromV2RepositoryContexts deep copies repository contexts.
func ConvertFromV2RepositoryContexts(contexts []runtime.Unstructured) []runtime.Unstructured {
	if contexts == nil {
		return nil
	}
	n := make([]runtime.Unstructured, len(contexts))
	for i := range contexts {
		contexts[i].DeepCopyInto(&n[i])
	}
	return n
}

// ConvertFromV2Labels maps v2 labels to internal Label format.
func ConvertFromV2Labels(labels []v2.Label) []Label {
	if labels == nil {
		return nil
	}
	n := make([]Label, len(labels))
	for i := range labels {
		n[i] = Label{
			Name:    labels[i].Name,
			Value:   labels[i].Value,
			Signing: labels[i].Signing,
		}
	}
	return n
}

// ConvertFromV2Resources maps v2 resources to internal Resource format.
func ConvertFromV2Resources(resources []v2.Resource) []Resource {
	if resources == nil {
		return nil
	}
	n := make([]Resource, len(resources))
	for i := range resources {
		r := resources[i]
		n[i] = Resource{
			ElementMeta: ElementMeta{
				ObjectMeta: ObjectMeta{
					Name:    r.Name,
					Version: r.Version,
				},
				ExtraIdentity: r.ExtraIdentity.DeepCopy(),
			},
			Type:     r.Type,
			Size:     r.Size,
			Relation: ResourceRelation(r.Relation),
			Access:   r.Access.DeepCopy(),
		}
		if r.CreationTime != nil {
			n[i].CreationTime = CreationTime(r.CreationTime.Time.Time)
		}
		n[i].Labels = ConvertFromV2Labels(r.Labels)
		n[i].Digest = ConvertFromV2Digest(r.Digest)
		n[i].SourceRefs = ConvertFromV2SourceRefs(r.SourceRefs)
	}
	return n
}

// ConvertFromV2SourceRefs maps v2 SourceRefs to internal SourceRef.
func ConvertFromV2SourceRefs(refs []v2.SourceRef) []SourceRef {
	if refs == nil {
		return nil
	}
	n := make([]SourceRef, len(refs))
	for i := range refs {
		n[i] = SourceRef{
			IdentitySelector: maps.Clone(refs[i].IdentitySelector),
			Labels:           ConvertFromV2Labels(refs[i].Labels),
		}
	}
	return n
}

// ConvertFromV2Digest converts v2.Digest to internal Digest.
func ConvertFromV2Digest(digest *v2.Digest) *Digest {
	if digest == nil {
		return nil
	}
	return &Digest{
		HashAlgorithm:          digest.HashAlgorithm,
		NormalisationAlgorithm: digest.NormalisationAlgorithm,
		Value:                  digest.Value,
	}
}

// ConvertFromV2Sources maps v2 Sources to internal Source.
func ConvertFromV2Sources(sources []v2.Source) []Source {
	if sources == nil {
		return nil
	}
	n := make([]Source, len(sources))
	for i := range sources {
		s := sources[i]
		n[i] = Source{
			ElementMeta: ElementMeta{
				ObjectMeta: ObjectMeta{
					Name:    s.Name,
					Version: s.Version,
					Labels:  ConvertFromV2Labels(s.Labels),
				},
				ExtraIdentity: s.ExtraIdentity.DeepCopy(),
			},
			Type:   s.Type,
			Access: s.Access.DeepCopy(),
		}
	}
	return n
}

// ConvertFromV2References maps v2 References to internal Reference.
func ConvertFromV2References(references []v2.Reference) []Reference {
	if references == nil {
		return nil
	}
	n := make([]Reference, len(references))
	for i := range references {
		r := references[i]
		n[i] = Reference{
			ElementMeta: ElementMeta{
				ObjectMeta: ObjectMeta{
					Name:    r.Name,
					Version: r.Version,
					Labels:  ConvertFromV2Labels(r.Labels),
				},
				ExtraIdentity: r.ExtraIdentity.DeepCopy(),
			},
			Component: r.Component,
		}
	}
	return n
}

// ConvertFromV2Signatures maps v2.Signature to internal Signature.
func ConvertFromV2Signatures(signatures []v2.Signature) []Signature {
	if signatures == nil {
		return nil
	}
	n := make([]Signature, len(signatures))
	for i := range signatures {
		n[i] = Signature{
			Name:   signatures[i].Name,
			Digest: *ConvertFromV2Digest(&signatures[i].Digest),
			Signature: SignatureInfo{
				Algorithm: signatures[i].Signature.Algorithm,
				Value:     signatures[i].Signature.Value,
				MediaType: signatures[i].Signature.MediaType,
				Issuer:    signatures[i].Signature.Issuer,
			},
		}
	}
	return n
}

// ConvertToV2Provider converts runtime.Identity to a provider string.
func ConvertToV2Provider(provider runtime.Identity) (string, error) {
	if provider == nil {
		return "", nil
	}
	if name, ok := provider[v2.IdentityAttributeName]; ok {
		return name, nil
	}
	return "", fmt.Errorf("provider name not found")
}

// ConvertToV2RepositoryContexts deep copies repository contexts.
func ConvertToV2RepositoryContexts(contexts []runtime.Unstructured) []runtime.Unstructured {
	if contexts == nil {
		return nil
	}
	n := make([]runtime.Unstructured, len(contexts))
	for i := range contexts {
		contexts[i].DeepCopyInto(&n[i])
	}
	return n
}

// ConvertToV2Labels maps internal Label to v2.Label.
func ConvertToV2Labels(labels []Label) []v2.Label {
	if labels == nil {
		return nil
	}
	n := make([]v2.Label, len(labels))
	for i := range labels {
		n[i] = v2.Label{
			Name:    labels[i].Name,
			Value:   labels[i].Value,
			Signing: labels[i].Signing,
		}
	}
	return n
}

// ConvertToV2Resources maps internal Resource to v2.Resource.
func ConvertToV2Resources(resources []Resource) []v2.Resource {
	if resources == nil {
		return nil
	}
	n := make([]v2.Resource, len(resources))
	for i := range resources {
		r := resources[i]
		res := v2.Resource{
			ElementMeta: v2.ElementMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    r.Name,
					Version: r.Version,
					Labels:  ConvertToV2Labels(r.Labels),
				},
				ExtraIdentity: r.ExtraIdentity.DeepCopy(),
			},
			Type:       r.Type,
			Size:       r.Size,
			Relation:   v2.ResourceRelation(r.Relation),
			Access:     r.Access.DeepCopy(),
			Digest:     ConvertToV2Digest(r.Digest),
			SourceRefs: ConvertToV2SourceRefs(r.SourceRefs),
		}
		if time.Time(r.CreationTime) != (time.Time{}) {
			res.CreationTime = &v2.Timestamp{Time: v2.Time{Time: time.Time(r.CreationTime)}}
		}
		n[i] = res
	}
	return n
}

// ConvertToV2SourceRefs maps internal SourceRef to v2.SourceRef.
func ConvertToV2SourceRefs(refs []SourceRef) []v2.SourceRef {
	if refs == nil {
		return nil
	}
	n := make([]v2.SourceRef, len(refs))
	for i := range refs {
		n[i] = v2.SourceRef{
			IdentitySelector: maps.Clone(refs[i].IdentitySelector),
			Labels:           ConvertToV2Labels(refs[i].Labels),
		}
	}
	return n
}

// ConvertToV2Digest converts internal Digest to v2.Digest.
func ConvertToV2Digest(digest *Digest) *v2.Digest {
	if digest == nil {
		return nil
	}
	return &v2.Digest{
		HashAlgorithm:          digest.HashAlgorithm,
		NormalisationAlgorithm: digest.NormalisationAlgorithm,
		Value:                  digest.Value,
	}
}

// ConvertToV2Sources maps internal Source to v2.Source.
func ConvertToV2Sources(sources []Source) []v2.Source {
	if sources == nil {
		return nil
	}
	n := make([]v2.Source, len(sources))
	for i := range sources {
		s := sources[i]
		n[i] = v2.Source{
			ElementMeta: v2.ElementMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    s.Name,
					Version: s.Version,
					Labels:  ConvertToV2Labels(s.Labels),
				},
				ExtraIdentity: s.ExtraIdentity.DeepCopy(),
			},
			Type:   s.Type,
			Access: s.Access.DeepCopy(),
		}
	}
	return n
}

// ConvertToV2References maps internal Reference to v2.Reference.
func ConvertToV2References(references []Reference) []v2.Reference {
	if references == nil {
		return nil
	}
	n := make([]v2.Reference, len(references))
	for i := range references {
		r := references[i]
		n[i] = v2.Reference{
			ElementMeta: v2.ElementMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    r.Name,
					Version: r.Version,
					Labels:  ConvertToV2Labels(r.Labels),
				},
				ExtraIdentity: r.ExtraIdentity.DeepCopy(),
			},
			Component: r.Component,
		}
	}
	return n
}

// ConvertToV2Signatures maps internal Signature to v2.Signature.
func ConvertToV2Signatures(signatures []Signature) []v2.Signature {
	if signatures == nil {
		return nil
	}
	n := make([]v2.Signature, len(signatures))
	for i := range signatures {
		s := signatures[i]
		n[i] = v2.Signature{
			Name:   s.Name,
			Digest: *ConvertToV2Digest(&s.Digest),
			Signature: v2.SignatureInfo{
				Algorithm: s.Signature.Algorithm,
				Value:     s.Signature.Value,
				MediaType: s.Signature.MediaType,
				Issuer:    s.Signature.Issuer,
			},
		}
	}
	return n
}
