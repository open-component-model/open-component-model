package runtime

import (
	"encoding/json"
	"fmt"
	"maps"
	"time"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

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

func ConvertFromV2Provider(provider string) (runtime.Identity, error) {
	if json.Valid([]byte(provider)) {
		id := runtime.Identity{}
		if err := json.Unmarshal([]byte(provider), &id); err != nil {
			return nil, fmt.Errorf("could not unmarshal provider string: %w", err)
		}
		return id, nil
	}
	return runtime.Identity{
		v2.IdentityAttributeName: provider,
	}, nil
}

func ConvertFromV2RepositoryContexts(contexts []runtime.Unstructured) []runtime.Unstructured {
	if contexts == nil {
		return nil
	}
	n := make([]runtime.Unstructured, len(contexts))
	for i := range contexts {
		(&contexts[i]).DeepCopyInto(&n[i])
	}
	return n
}

func ConvertFromV2Labels(labels []v2.Label) []Label {
	if labels == nil {
		return nil
	}
	n := make([]Label, len(labels))
	for i := range labels {
		n[i].Name = labels[i].Name
		n[i].Value = labels[i].Value
		n[i].Signing = labels[i].Signing
	}
	return n
}

func ConvertFromV2Resources(resources []v2.Resource) []Resource {
	if resources == nil {
		return nil
	}
	n := make([]Resource, len(resources))
	for i := range resources {
		n[i].Name = resources[i].Name
		n[i].Version = resources[i].Version
		n[i].Type = resources[i].Type
		if resources[i].CreationTime != nil {
			n[i].CreationTime = CreationTime(resources[i].CreationTime.Time.Time)
		}
		if resources[i].Labels != nil {
			n[i].Labels = ConvertFromV2Labels(resources[i].Labels)
		}
		if resources[i].Digest != nil {
			n[i].Digest = ConvertFromV2Digest(resources[i].Digest)
		}
		if resources[i].SourceRefs != nil {
			n[i].SourceRefs = ConvertFromV2SourceRefs(resources[i].SourceRefs)
		}
		if resources[i].Access != nil {
			n[i].Access = resources[i].Access.DeepCopy()
		}
		if resources[i].ExtraIdentity != nil {
			n[i].ExtraIdentity = resources[i].ExtraIdentity.DeepCopy()
		}
		n[i].Size = resources[i].Size
		n[i].Relation = ResourceRelation(resources[i].Relation)
	}
	return n
}

func ConvertFromV2SourceRefs(refs []v2.SourceRef) []SourceRef {
	if refs == nil {
		return nil
	}
	n := make([]SourceRef, len(refs))
	for i := range refs {
		n[i].IdentitySelector = maps.Clone(refs[i].IdentitySelector)
		n[i].Labels = ConvertFromV2Labels(refs[i].Labels)
	}
	return n
}

func ConvertFromV2Digest(digest *v2.Digest) *Digest {
	return &Digest{
		HashAlgorithm:          digest.HashAlgorithm,
		NormalisationAlgorithm: digest.NormalisationAlgorithm,
		Value:                  digest.Value,
	}
}

func ConvertFromV2Sources(sources []v2.Source) []Source {
	if sources == nil {
		return nil
	}
	n := make([]Source, len(sources))
	for i := range sources {
		n[i].Name = sources[i].Name
		n[i].Version = sources[i].Version
		n[i].Labels = ConvertFromV2Labels(sources[i].Labels)
		n[i].ExtraIdentity = sources[i].ExtraIdentity.DeepCopy()
		n[i].Access = sources[i].Access.DeepCopy()
		n[i].Type = sources[i].Type
	}
	return n
}

func ConvertFromV2References(references []v2.Reference) []Reference {
	if references == nil {
		return nil
	}
	n := make([]Reference, len(references))
	for i := range references {
		n[i].Name = references[i].Name
		n[i].Version = references[i].Version
		n[i].Labels = ConvertFromV2Labels(references[i].Labels)
		n[i].ExtraIdentity = references[i].ExtraIdentity.DeepCopy()
		n[i].Component = references[i].Component
	}
	return n
}

func ConvertFromV2Signatures(signatures []v2.Signature) []Signature {
	if signatures == nil {
		return nil
	}
	n := make([]Signature, len(signatures))
	for i := range signatures {
		n[i].Name = signatures[i].Name
		n[i].Digest = *ConvertFromV2Digest(&signatures[i].Digest)
		n[i].Signature = SignatureInfo{
			Algorithm: signatures[i].Signature.Algorithm,
			Value:     signatures[i].Signature.Value,
			MediaType: signatures[i].Signature.MediaType,
			Issuer:    signatures[i].Signature.Issuer,
		}
	}
	return n
}

func ConvertToV2Provider(provider runtime.Identity) (string, error) {
	if provider == nil {
		return "", nil
	}
	if name, ok := provider[v2.IdentityAttributeName]; ok {
		return name, nil
	}
	return "", fmt.Errorf("provider name not found")
}

func ConvertToV2RepositoryContexts(contexts []runtime.Unstructured) []runtime.Unstructured {
	if contexts == nil {
		return nil
	}
	n := make([]runtime.Unstructured, len(contexts))
	for i := range contexts {
		(&contexts[i]).DeepCopyInto(&n[i])
	}
	return n
}

func ConvertToV2Labels(labels []Label) []v2.Label {
	if labels == nil {
		return nil
	}
	n := make([]v2.Label, len(labels))
	for i := range labels {
		n[i].Name = labels[i].Name
		n[i].Value = labels[i].Value
		n[i].Signing = labels[i].Signing
	}
	return n
}

func ConvertToV2Resources(resources []Resource) []v2.Resource {
	if resources == nil {
		return nil
	}
	n := make([]v2.Resource, len(resources))
	for i := range resources {
		n[i].Name = resources[i].Name
		n[i].Version = resources[i].Version
		n[i].Type = resources[i].Type
		if time.Time(resources[i].CreationTime) != (time.Time{}) {
			n[i].CreationTime = &v2.Timestamp{Time: v2.Time{Time: time.Time(resources[i].CreationTime)}}
		}
		n[i].Labels = ConvertToV2Labels(resources[i].Labels)
		n[i].Digest = ConvertToV2Digest(resources[i].Digest)
		n[i].SourceRefs = ConvertToV2SourceRefs(resources[i].SourceRefs)
		n[i].Access = resources[i].Access.DeepCopy()
		n[i].ExtraIdentity = resources[i].ExtraIdentity.DeepCopy()
		n[i].Size = resources[i].Size
		n[i].Relation = v2.ResourceRelation(resources[i].Relation)
	}
	return n
}

func ConvertToV2SourceRefs(refs []SourceRef) []v2.SourceRef {
	if refs == nil {
		return nil
	}
	n := make([]v2.SourceRef, len(refs))
	for i := range refs {
		n[i].IdentitySelector = maps.Clone(refs[i].IdentitySelector)
		n[i].Labels = ConvertToV2Labels(refs[i].Labels)
	}
	return n
}

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

func ConvertToV2Sources(sources []Source) []v2.Source {
	if sources == nil {
		return nil
	}
	n := make([]v2.Source, len(sources))
	for i := range sources {
		n[i].Name = sources[i].Name
		n[i].Version = sources[i].Version
		n[i].Labels = ConvertToV2Labels(sources[i].Labels)
		n[i].ExtraIdentity = sources[i].ExtraIdentity.DeepCopy()
		n[i].Access = sources[i].Access.DeepCopy()
		n[i].Type = sources[i].Type
	}
	return n
}

func ConvertToV2References(references []Reference) []v2.Reference {
	if references == nil {
		return nil
	}
	n := make([]v2.Reference, len(references))
	for i := range references {
		n[i].Name = references[i].Name
		n[i].Version = references[i].Version
		n[i].Labels = ConvertToV2Labels(references[i].Labels)
		n[i].ExtraIdentity = references[i].ExtraIdentity.DeepCopy()
		n[i].Component = references[i].Component
	}
	return n
}

func ConvertToV2Signatures(signatures []Signature) []v2.Signature {
	if signatures == nil {
		return nil
	}
	n := make([]v2.Signature, len(signatures))
	for i := range signatures {
		n[i].Name = signatures[i].Name
		n[i].Digest = *ConvertToV2Digest(&signatures[i].Digest)
		n[i].Signature = v2.SignatureInfo{
			Algorithm: signatures[i].Signature.Algorithm,
			Value:     signatures[i].Signature.Value,
			MediaType: signatures[i].Signature.MediaType,
			Issuer:    signatures[i].Signature.Issuer,
		}
	}
	return n
}
