package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ConvertFromV2 converts a v2.Descriptor to the internal Descriptor format.
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

// ConvertToV2 converts an internal Descriptor to a v2.Descriptor format.
func ConvertToV2(scheme *runtime.Scheme, descriptor *Descriptor) (*v2.Descriptor, error) {
	provider, err := ConvertToV2Provider(descriptor.Component.Provider)
	if err != nil {
		return nil, err
	}

	res, err := ConvertToV2Resources(scheme, descriptor.Component.Resources)
	if err != nil {
		return nil, fmt.Errorf("could not convert resources: %w", err)
	}

	srcs, err := ConvertToV2Sources(scheme, descriptor.Component.Sources)
	if err != nil {
		return nil, fmt.Errorf("could not convert sources: %w", err)
	}

	repoCtx, err := ConvertToV2RepositoryContexts(scheme, descriptor.Component.RepositoryContexts)
	if err != nil {
		return nil, fmt.Errorf("could not convert repository contexts: %w", err)
	}

	labels, err := ConvertToV2Labels(descriptor.Component.Labels)
	if err != nil {
		return nil, fmt.Errorf("could not convert component labels: %w", err)
	}

	references, err := ConvertToV2References(descriptor.Component.References)
	if err != nil {
		return nil, fmt.Errorf("could not convert references: %w", err)
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
					Labels:  labels,
				},
				CreationTime: descriptor.Component.CreationTime,
			},
			RepositoryContexts: repoCtx,
			Provider:           provider,
			Resources:          res,
			Sources:            srcs,
			References:         references,
		},
		Signatures: ConvertToV2Signatures(descriptor.Signatures),
	}, nil
}

// ConvertFromV2Provider parses a provider string to an Identity map or JSON structure.
func ConvertFromV2Provider(provider string) (Provider, error) {
	if provider == "" {
		return Provider{}, nil
	}
	if strings.HasPrefix(strings.TrimSpace(provider), "{") {
		if !json.Valid([]byte(provider)) {
			return Provider{}, fmt.Errorf("invalid JSON format")
		}
		type providerStruct struct {
			Name   string     `json:"name"`
			Labels []v2.Label `json:"labels,omitempty"`
		}
		var id providerStruct
		if err := json.Unmarshal([]byte(provider), &id); err != nil {
			return Provider{}, fmt.Errorf("could not unmarshal provider string: %w", err)
		}
		return Provider{
			Name:   id.Name,
			Labels: ConvertFromV2Labels(id.Labels),
		}, nil
	}
	// If not JSON, fallback to a single key map.
	return Provider{
		Name: provider,
	}, nil
}

// ConvertFromV2RepositoryContexts deep copies a slice of unstructured repository contexts.
func ConvertFromV2RepositoryContexts(contexts []*runtime.Raw) []runtime.Typed {
	if contexts == nil {
		return nil
	}
	n := make([]runtime.Typed, len(contexts))
	for i := range contexts {
		n[i] = contexts[i].DeepCopy()
	}
	return n
}

// ConvertFromV2Labels converts a list of v2.Label to internal Label.
func ConvertFromV2Labels(labels []v2.Label) []Label {
	if labels == nil {
		return nil
	}
	n := make([]Label, len(labels))
	for i := range labels {
		n[i].Name = labels[i].Name
		n[i].Value = bytes.Clone(labels[i].Value)
		n[i].Signing = labels[i].Signing
	}
	return n
}

// ConvertFromV2Resources converts v2 resources to internal representation.
func ConvertFromV2Resources(resources []v2.Resource) []Resource {
	if resources == nil {
		return nil
	}
	n := make([]Resource, len(resources))
	for i := range resources {
		n[i] = *ConvertFromV2Resource(&resources[i])
	}
	return n
}

// ConvertFromV2Resource converts v2 resources to internal representation.
func ConvertFromV2Resource(res *v2.Resource) *Resource {
	if res == nil {
		return nil
	}
	var resource Resource
	resource.Name = res.Name
	resource.Version = res.Version
	resource.Type = res.Type
	if res.CreationTime != nil {
		resource.CreationTime = CreationTime(res.CreationTime.Time.Time)
	}
	if res.Labels != nil {
		resource.Labels = ConvertFromV2Labels(res.Labels)
	}
	if res.Digest != nil {
		resource.Digest = ConvertFromV2Digest(res.Digest)
	}
	if res.SourceRefs != nil {
		resource.SourceRefs = ConvertFromV2SourceRefs(res.SourceRefs)
	}
	if res.Access != nil {
		resource.Access = res.Access.DeepCopy()
	}
	if res.ExtraIdentity != nil {
		resource.ExtraIdentity = res.ExtraIdentity.DeepCopy()
	}
	resource.Relation = ResourceRelation(res.Relation)

	return &resource
}

// ConvertFromV2SourceRefs converts v2 source references to internal format.
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

// ConvertFromV2Digest converts a v2.Digest to internal Digest.
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

// ConvertFromV2Sources converts v2 sources to internal sources.
func ConvertFromV2Sources(sources []v2.Source) []Source {
	if sources == nil {
		return nil
	}
	n := make([]Source, len(sources))
	for i := range sources {
		n[i].ElementMeta = ElementMeta{
			ObjectMeta: ObjectMeta{
				Name:    sources[i].Name,
				Version: sources[i].Version,
				Labels:  ConvertFromV2Labels(sources[i].Labels),
			},
			ExtraIdentity: sources[i].ExtraIdentity.DeepCopy(),
		}
		if sources[i].Access != nil {
			n[i].Access = sources[i].Access.DeepCopy()
		}
		n[i].Type = sources[i].Type
	}
	return n
}

// ConvertFromV2References converts v2 references to internal references.
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
		n[i].Digest = *ConvertFromV2Digest(&references[i].Digest)
	}
	return n
}

// ConvertFromV2Signatures converts v2 signatures to internal format.
func ConvertFromV2Signatures(signatures []v2.Signature) []Signature {
	if signatures == nil {
		return nil
	}
	n := make([]Signature, len(signatures))
	for i := range signatures {
		n[i] = *ConvertFromV2Signature(&signatures[i])
	}
	return n
}

func ConvertFromV2Signature(signature *v2.Signature) *Signature {
	if signature == nil {
		return nil
	}
	return &Signature{
		Name:      signature.Name,
		Digest:    *ConvertFromV2Digest(&signature.Digest),
		Signature: *ConvertFromV2SignatureInfo(&signature.Signature),
	}
}

func ConvertFromV2SignatureInfo(signature *v2.SignatureInfo) *SignatureInfo {
	if signature == nil {
		return nil
	}
	return &SignatureInfo{
		Algorithm: signature.Algorithm,
		Value:     signature.Value,
		MediaType: signature.MediaType,
		Issuer:    signature.Issuer,
	}
}

// ConvertToV2Provider converts an internal provider identity to a string format expected by v2.
func ConvertToV2Provider(provider Provider) (string, error) {
	if provider.Name == "" {
		return "", fmt.Errorf("provider name is empty")
	}
	if provider.Labels == nil {
		return provider.Name, nil
	}
	type providerStruct struct {
		Name   string     `json:"name"`
		Labels []v2.Label `json:"labels,omitempty"`
	}
	labels, err := ConvertToV2Labels(provider.Labels)
	if err != nil {
		return "", fmt.Errorf("could not convert provider labels: %w", err)
	}
	providerJSON, err := json.Marshal(&providerStruct{
		Name:   provider.Name,
		Labels: labels,
	})
	if err != nil {
		return "", fmt.Errorf("could not marshal provider to JSON: %w", err)
	}
	return string(providerJSON), nil
}

// ConvertToV2RepositoryContexts deep copies internal repository contexts to v2 format.
func ConvertToV2RepositoryContexts(scheme *runtime.Scheme, contexts []runtime.Typed) ([]*runtime.Raw, error) {
	if contexts == nil {
		return nil, nil
	}
	n := make([]*runtime.Raw, len(contexts))
	for i := range contexts {
		n[i] = &runtime.Raw{}
		if err := scheme.Convert(contexts[i], n[i]); err != nil {
			return nil, fmt.Errorf("could not convert repository context at index %d: %w", i, err)
		}
	}
	return n, nil
}

// ConvertToV2Labels converts internal labels to v2.Label format.
func ConvertToV2Labels(labels []Label) ([]v2.Label, error) {
	if labels == nil {
		return nil, nil
	}
	n := make([]v2.Label, len(labels))
	for i := range labels {
		n[i].Name = labels[i].Name
		n[i].Value = bytes.Clone(labels[i].Value)
		n[i].Signing = labels[i].Signing
	}
	return n, nil
}

// ConvertToV2Resources converts internal resources to v2 resources.
func ConvertToV2Resources(scheme *runtime.Scheme, resources []Resource) ([]v2.Resource, error) {
	if resources == nil {
		return nil, nil
	}
	n := make([]v2.Resource, len(resources))
	for i := range resources {
		resource, err := ConvertToV2Resource(scheme, &resources[i])
		if err != nil {
			return nil, fmt.Errorf("could not convert resource %q: %w", resources[i].Name, err)
		}
		n[i] = *resource
	}
	return n, nil
}

// ConvertToV2Resource converts an internal resource to a v2 resource.
func ConvertToV2Resource(scheme *runtime.Scheme, res *Resource) (*v2.Resource, error) {
	if res == nil {
		return nil, nil
	}
	var resource v2.Resource
	resource.Name = res.Name
	resource.Version = res.Version
	resource.Type = res.Type
	if time.Time(res.CreationTime) != (time.Time{}) {
		resource.CreationTime = &v2.Timestamp{Time: v2.Time{Time: time.Time(res.CreationTime)}}
	}
	l, err := ConvertToV2Labels(res.Labels)
	if err != nil {
		return nil, fmt.Errorf("could not convert labels for resource %q: %w", res.Name, err)
	}
	resource.Labels = l
	resource.Digest = ConvertToV2Digest(res.Digest)
	srcRefs, err := ConvertToV2SourceRefs(res.SourceRefs)
	if err != nil {
		return nil, fmt.Errorf("could not convert source refs for resource %q: %w", res.Name, err)
	}
	resource.SourceRefs = srcRefs
	resource.Access = &runtime.Raw{}
	// Use runtime.Scheme to convert custom access types.
	if err := scheme.Convert(res.Access, resource.Access); err != nil {
		return nil, fmt.Errorf("could not convert access %q: %w", res.String(), err)
	}
	resource.ExtraIdentity = res.ExtraIdentity.DeepCopy()
	resource.Relation = v2.ResourceRelation(res.Relation)

	return &resource, nil
}

// ConvertToV2SourceRefs converts internal source references to v2 format.
func ConvertToV2SourceRefs(refs []SourceRef) ([]v2.SourceRef, error) {
	if refs == nil {
		return nil, nil
	}
	n := make([]v2.SourceRef, len(refs))
	for i := range refs {
		n[i].IdentitySelector = maps.Clone(refs[i].IdentitySelector)
		l, err := ConvertToV2Labels(refs[i].Labels)
		if err != nil {
			return nil, fmt.Errorf("could not convert labels for source ref %q: %w", refs[i].IdentitySelector, err)
		}
		n[i].Labels = l
	}
	return n, nil
}

// ConvertToV2Digest converts an internal digest to v2 format.
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

// ConvertToV2Sources converts internal sources to v2 sources.
func ConvertToV2Sources(scheme *runtime.Scheme, sources []Source) ([]v2.Source, error) {
	if sources == nil {
		return nil, nil
	}
	n := make([]v2.Source, len(sources))
	for i := range sources {
		n[i].Name = sources[i].Name
		n[i].Version = sources[i].Version
		l, err := ConvertToV2Labels(sources[i].Labels)
		if err != nil {
			return nil, fmt.Errorf("could not convert labels for source %q: %w", sources[i].Name, err)
		}
		n[i].Labels = l
		n[i].ExtraIdentity = sources[i].ExtraIdentity.DeepCopy()
		n[i].Access = &runtime.Raw{}
		// Use runtime.Scheme to convert custom access types.
		if err := scheme.Convert(sources[i].Access, n[i].Access); err != nil {
			return nil, fmt.Errorf("could not convert access %q: %w", sources[i].String(), err)
		}
		n[i].Type = sources[i].Type
	}
	return n, nil
}

// ConvertToV2References converts internal references to v2 references.
func ConvertToV2References(references []Reference) ([]v2.Reference, error) {
	if references == nil {
		return nil, nil
	}
	n := make([]v2.Reference, len(references))
	for i := range references {
		n[i].Name = references[i].Name
		n[i].Version = references[i].Version
		l, err := ConvertToV2Labels(references[i].Labels)
		if err != nil {
			return nil, fmt.Errorf("could not convert labels for reference %q: %w", references[i].Name, err)
		}
		n[i].Labels = l
		n[i].ExtraIdentity = references[i].ExtraIdentity.DeepCopy()
		n[i].Component = references[i].Component
		n[i].Digest = *ConvertToV2Digest(&references[i].Digest)
	}
	return n, nil
}

// ConvertToV2Signatures converts internal signatures to v2 format.
func ConvertToV2Signatures(signatures []Signature) []v2.Signature {
	if signatures == nil {
		return nil
	}
	n := make([]v2.Signature, len(signatures))
	for i := range signatures {
		n[i] = *ConvertToV2Signature(&signatures[i])
	}
	return n
}

func ConvertToV2Signature(sig *Signature) *v2.Signature {
	if sig == nil {
		return nil
	}
	return &v2.Signature{
		Name:      sig.Name,
		Digest:    *ConvertToV2Digest(&sig.Digest),
		Signature: *ConvertToV2SignatureInfo(&sig.Signature),
	}
}

func ConvertToV2SignatureInfo(sig *SignatureInfo) *v2.SignatureInfo {
	if sig == nil {
		return nil
	}
	return &v2.SignatureInfo{
		Algorithm: sig.Algorithm,
		Value:     sig.Value,
		MediaType: sig.MediaType,
		Issuer:    sig.Issuer,
	}
}

// ConvertFromV2LocalBlob converts a v2.LocalBlob to runtime.LocalBlob.
func ConvertFromV2LocalBlob(scheme *runtime.Scheme, blob *v2.LocalBlob) (*LocalBlob, error) {
	if blob == nil {
		return nil, nil
	}
	result := &LocalBlob{
		Type:           blob.Type,
		LocalReference: blob.LocalReference,
		MediaType:      blob.MediaType,
		ReferenceName:  blob.ReferenceName,
	}
	if blob.GlobalAccess != nil {
		result.GlobalAccess = blob.GlobalAccess.DeepCopy()
	}
	return result, nil
}

// ConvertToV2LocalBlob converts a runtime.LocalBlob to v2.LocalBlob.
func ConvertToV2LocalBlob(scheme *runtime.Scheme, blob *LocalBlob) (*v2.LocalBlob, error) {
	if blob == nil {
		return nil, nil
	}
	result := &v2.LocalBlob{
		Type:           blob.Type,
		LocalReference: blob.LocalReference,
		MediaType:      blob.MediaType,
		ReferenceName:  blob.ReferenceName,
	}
	if blob.GlobalAccess != nil {
		result.GlobalAccess = &runtime.Raw{}
		if err := scheme.Convert(blob.GlobalAccess, result.GlobalAccess); err != nil {
			return nil, fmt.Errorf("could not convert global access: %w", err)
		}
	}
	return result, nil
}
