package runtime

import (
	"maps"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Label conversion functions

func ConvertToDescriptorLabels(labels []Label) []descriptor.Label {
	if labels == nil {
		return nil
	}
	n := make([]descriptor.Label, len(labels))
	for i := range labels {
		n[i].Name = labels[i].Name
		n[i].Value = labels[i].Value
		n[i].Signing = labels[i].Signing
	}
	return n
}

func ConvertFromDescriptorLabels(labels []descriptor.Label) []Label {
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

// Common conversion helpers

func convertObjectMetaToDescriptor(meta ObjectMeta) descriptor.ObjectMeta {
	return descriptor.ObjectMeta{
		Name:    meta.Name,
		Version: meta.Version,
		Labels:  ConvertToDescriptorLabels(meta.Labels),
	}
}

func convertObjectMetaFromDescriptor(meta descriptor.ObjectMeta) ObjectMeta {
	return ObjectMeta{
		Name:    meta.Name,
		Version: meta.Version,
		Labels:  ConvertFromDescriptorLabels(meta.Labels),
	}
}

func convertElementMetaToDescriptor(meta ElementMeta) descriptor.ElementMeta {
	return descriptor.ElementMeta{
		ObjectMeta:    convertObjectMetaToDescriptor(meta.ObjectMeta),
		ExtraIdentity: meta.ExtraIdentity.DeepCopy(),
	}
}

func convertElementMetaFromDescriptor(meta descriptor.ElementMeta) ElementMeta {
	return ElementMeta{
		ObjectMeta:    convertObjectMetaFromDescriptor(meta.ObjectMeta),
		ExtraIdentity: meta.ExtraIdentity.Clone(),
	}
}

func convertAccessToDescriptor(accessOrInput AccessOrInput) runtime.Typed {
	if accessOrInput.Access == nil {
		return nil
	}
	return accessOrInput.Access.DeepCopyTyped()
}

func convertAccessFromDescriptor(access runtime.Typed) AccessOrInput {
	if access == nil {
		return AccessOrInput{}
	}
	return AccessOrInput{
		Access: access.DeepCopyTyped(),
	}
}

// Resource conversion

func ConvertFromDescriptorResource(resource *descriptor.Resource) *Resource {
	if resource == nil {
		return nil
	}
	target := &Resource{
		ElementMeta: convertElementMetaFromDescriptor(resource.ElementMeta),
		Type:        resource.Type,
		Relation:    ResourceRelation(resource.Relation),
	}

	if resource.SourceRefs != nil {
		target.SourceRefs = ConvertFromDescriptorSourceRefs(resource.SourceRefs)
	}

	target.AccessOrInput = convertAccessFromDescriptor(resource.Access)
	return target
}

func ConvertToDescriptorResource(resource *Resource) *descriptor.Resource {
	if resource == nil {
		return nil
	}
	target := &descriptor.Resource{
		ElementMeta: convertElementMetaToDescriptor(resource.ElementMeta),
		Type:        resource.Type,
		Relation:    descriptor.ResourceRelation(resource.Relation),
	}

	if resource.SourceRefs != nil {
		target.SourceRefs = ConvertToDescriptorSourceRefs(resource.SourceRefs)
	}

	target.Access = convertAccessToDescriptor(resource.AccessOrInput)
	return target
}

// Source conversion

func ConvertToDescriptorSource(source *Source) *descriptor.Source {
	if source == nil {
		return nil
	}
	target := &descriptor.Source{
		ElementMeta: convertElementMetaToDescriptor(source.ElementMeta),
		Type:        source.Type,
		Access:      convertAccessToDescriptor(source.AccessOrInput),
	}
	return target
}

// Reference conversion

func ConvertToDescriptorReference(reference *Reference) *descriptor.Reference {
	if reference == nil {
		return nil
	}
	target := &descriptor.Reference{
		ElementMeta: convertElementMetaToDescriptor(reference.ElementMeta),
		Component:   reference.Component,
	}
	return target
}

// Provider conversion
func convertProviderToDescriptor(provider Provider) (descriptor.Provider, error) {
	return descriptor.Provider{
		Name:   provider.Name,
		Labels: ConvertToDescriptorLabels(provider.Labels),
	}, nil
}

// Component conversion

func ConvertToDescriptorComponent(component *Component) *descriptor.Component {
	if component == nil {
		return nil
	}

	provider, err := convertProviderToDescriptor(component.Provider)
	if err != nil {
		return nil
	}

	target := &descriptor.Component{
		ComponentMeta: descriptor.ComponentMeta{
			ObjectMeta:   convertObjectMetaToDescriptor(component.ObjectMeta),
			CreationTime: component.CreationTime,
		},
		Provider: provider,
	}

	if component.Resources != nil {
		target.Resources = make([]descriptor.Resource, len(component.Resources))
		for i, resource := range component.Resources {
			if converted := ConvertToDescriptorResource(&resource); converted != nil {
				target.Resources[i] = *converted
			}
		}
	}

	if component.Sources != nil {
		target.Sources = make([]descriptor.Source, len(component.Sources))
		for i, source := range component.Sources {
			if converted := ConvertToDescriptorSource(&source); converted != nil {
				target.Sources[i] = *converted
			}
		}
	}

	if component.References != nil {
		target.References = make([]descriptor.Reference, len(component.References))
		for i, reference := range component.References {
			if converted := ConvertToDescriptorReference(&reference); converted != nil {
				target.References[i] = *converted
			}
		}
	}

	return target
}

// Constructor conversion

func ConvertToDescriptor(constructor *ComponentConstructor) *descriptor.Descriptor {
	if constructor == nil || len(constructor.Components) == 0 {
		return nil
	}
	component := ConvertToDescriptorComponent(&constructor.Components[0])
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v2",
		},
		Component: *component,
	}
}

// SourceRef conversion

func ConvertToDescriptorSourceRefs(refs []SourceRef) []descriptor.SourceRef {
	if refs == nil {
		return nil
	}
	n := make([]descriptor.SourceRef, len(refs))
	for i := range refs {
		n[i].IdentitySelector = maps.Clone(refs[i].IdentitySelector)
		n[i].Labels = ConvertToDescriptorLabels(refs[i].Labels)
	}
	return n
}

func ConvertFromDescriptorSourceRefs(refs []descriptor.SourceRef) []SourceRef {
	if refs == nil {
		return nil
	}
	n := make([]SourceRef, len(refs))
	for i := range refs {
		n[i].IdentitySelector = maps.Clone(refs[i].IdentitySelector)
		n[i].Labels = ConvertFromDescriptorLabels(refs[i].Labels)
	}
	return n
}
