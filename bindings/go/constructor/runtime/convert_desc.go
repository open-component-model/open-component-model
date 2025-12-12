package runtime

import (
	"maps"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Label conversion functions

// ConvertToDescriptorLabels converts a slice of runtime Labels to descriptor Labels.
// Returns nil if the input slice is nil.
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

// ConvertFromDescriptorLabels converts a slice of descriptor Labels to runtime Labels.
// Returns nil if the input slice is nil.
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

// ConvertObjectMetaToDescriptor converts runtime ObjectMeta to descriptor ObjectMeta.
func ConvertObjectMetaToDescriptor(meta ObjectMeta) descriptor.ObjectMeta {
	return descriptor.ObjectMeta{
		Name:    meta.Name,
		Version: meta.Version,
		Labels:  ConvertToDescriptorLabels(meta.Labels),
	}
}

// ConvertObjectMetaFromDescriptor converts descriptor ObjectMeta to runtime ObjectMeta.
func ConvertObjectMetaFromDescriptor(meta descriptor.ObjectMeta) ObjectMeta {
	return ObjectMeta{
		Name:    meta.Name,
		Version: meta.Version,
		Labels:  ConvertFromDescriptorLabels(meta.Labels),
	}
}

// ConvertElementMetaToDescriptor converts runtime ElementMeta to descriptor ElementMeta.
func ConvertElementMetaToDescriptor(meta ElementMeta) descriptor.ElementMeta {
	return descriptor.ElementMeta{
		ObjectMeta:    ConvertObjectMetaToDescriptor(meta.ObjectMeta),
		ExtraIdentity: meta.ExtraIdentity.DeepCopy(),
	}
}

// ConvertElementMetaFromDescriptor converts descriptor ElementMeta to runtime ElementMeta.
func ConvertElementMetaFromDescriptor(meta descriptor.ElementMeta) ElementMeta {
	return ElementMeta{
		ObjectMeta:    ConvertObjectMetaFromDescriptor(meta.ObjectMeta),
		ExtraIdentity: meta.ExtraIdentity.Clone(),
	}
}

// ConvertAccessToDescriptor converts runtime AccessOrInput to descriptor Typed.
// Returns nil if the input Access is nil.
func ConvertAccessToDescriptor(accessOrInput AccessOrInput) runtime.Typed {
	if accessOrInput.Access == nil {
		return nil
	}
	return accessOrInput.Access.DeepCopyTyped()
}

// ConvertAccessFromDescriptor converts descriptor Typed to runtime AccessOrInput.
// Returns empty AccessOrInput if the input is nil.
func ConvertAccessFromDescriptor(access runtime.Typed) AccessOrInput {
	if access == nil {
		return AccessOrInput{}
	}
	return AccessOrInput{
		Access: access.DeepCopyTyped(),
	}
}

// Resource conversion

// ConvertFromDescriptorResource converts a descriptor Resource to runtime Resource.
// Returns nil if the input is nil.
func ConvertFromDescriptorResource(resource *descriptor.Resource) *Resource {
	if resource == nil {
		return nil
	}
	target := &Resource{
		ElementMeta: ConvertElementMetaFromDescriptor(resource.ElementMeta),
		Type:        resource.Type,
		Relation:    ResourceRelation(resource.Relation),
	}

	if resource.SourceRefs != nil {
		target.SourceRefs = ConvertFromDescriptorSourceRefs(resource.SourceRefs)
	}

	target.AccessOrInput = ConvertAccessFromDescriptor(resource.Access)
	return target
}

// ConvertToDescriptorResource converts a runtime Resource to descriptor Resource.
// Returns nil if the input is nil.
func ConvertToDescriptorResource(resource *Resource) *descriptor.Resource {
	if resource == nil {
		return nil
	}
	target := &descriptor.Resource{
		ElementMeta: ConvertElementMetaToDescriptor(resource.ElementMeta),
		Type:        resource.Type,
		Relation:    descriptor.ResourceRelation(resource.Relation),
	}

	if resource.SourceRefs != nil {
		target.SourceRefs = ConvertToDescriptorSourceRefs(resource.SourceRefs)
	}

	target.Access = ConvertAccessToDescriptor(resource.AccessOrInput)
	return target
}

// Source conversion

// ConvertToDescriptorSource converts a runtime Source to descriptor Source.
// Returns nil if the input is nil.
func ConvertToDescriptorSource(source *Source) *descriptor.Source {
	if source == nil {
		return nil
	}
	target := &descriptor.Source{
		ElementMeta: ConvertElementMetaToDescriptor(source.ElementMeta),
		Type:        source.Type,
		Access:      ConvertAccessToDescriptor(source.AccessOrInput),
	}
	return target
}

// Reference conversion

// ConvertToDescriptorReference converts a runtime Reference to descriptor Reference.
// Returns nil if the input is nil.
func ConvertToDescriptorReference(reference *Reference) *descriptor.Reference {
	if reference == nil {
		return nil
	}
	target := &descriptor.Reference{
		ElementMeta: ConvertElementMetaToDescriptor(reference.ElementMeta),
		Component:   reference.Component,
	}
	return target
}

// ConvertFromDescriptorSource converts a descriptor Source to runtime Source.
// Returns nil if the input is nil.
func ConvertFromDescriptorSource(source *descriptor.Source) *Source {
	if source == nil {
		return nil
	}
	target := &Source{
		ElementMeta: ConvertElementMetaFromDescriptor(source.ElementMeta),
		Type:        source.Type,
	}
	if source.Access != nil {
		target.AccessOrInput = AccessOrInput{
			Access: source.Access.DeepCopyTyped(),
		}
	}
	return target
}

// ConvertFromDescriptorReference converts a descriptor Reference to runtime Reference.
// Returns nil if the input is nil.
func ConvertFromDescriptorReference(reference *descriptor.Reference) *Reference {
	if reference == nil {
		return nil
	}
	target := &Reference{
		ElementMeta: ConvertElementMetaFromDescriptor(reference.ElementMeta),
		Component:   reference.Component,
	}
	return target
}

// Provider conversion

// ConvertProviderToDescriptor converts runtime Provider to descriptor Provider.
// Returns an error if the conversion fails.
func ConvertProviderToDescriptor(provider Provider) (descriptor.Provider, error) {
	return descriptor.Provider{
		Name:   provider.Name,
		Labels: ConvertToDescriptorLabels(provider.Labels),
	}, nil
}

// Component conversion

// ConvertToDescriptorComponent converts a runtime Component to descriptor Component.
// Returns nil if the input is nil.
func ConvertToDescriptorComponent(component *Component) *descriptor.Component {
	if component == nil {
		return nil
	}

	provider := descriptor.Provider{
		Name:   component.Provider.Name,
		Labels: ConvertToDescriptorLabels(component.Provider.Labels),
	}

	target := &descriptor.Component{
		ComponentMeta: descriptor.ComponentMeta{
			ObjectMeta:   ConvertObjectMetaToDescriptor(component.ObjectMeta),
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

// ConvertFromDescriptorComponent converts a descriptor Component to runtime Component.
// Returns nil if the input is nil.
func ConvertFromDescriptorComponent(component *descriptor.Component) *Component {
	if component == nil {
		return nil
	}

	provider := Provider{
		Name:   component.Provider.Name,
		Labels: ConvertFromDescriptorLabels(component.Provider.Labels),
	}

	target := &Component{
		ComponentMeta: ComponentMeta{
			ObjectMeta:   ConvertObjectMetaFromDescriptor(component.ObjectMeta),
			CreationTime: component.CreationTime,
		},
		Provider: provider,
	}

	if component.Resources != nil {
		target.Resources = make([]Resource, len(component.Resources))
		for i, resource := range component.Resources {
			if converted := ConvertFromDescriptorResource(&resource); converted != nil {
				target.Resources[i] = *converted
			}
		}
	}

	if component.Sources != nil {
		target.Sources = make([]Source, len(component.Sources))
		for i, source := range component.Sources {
			if converted := ConvertFromDescriptorSource(&source); converted != nil {
				target.Sources[i] = *converted
			}
		}
	}

	if component.References != nil {
		target.References = make([]Reference, len(component.References))
		for i, reference := range component.References {
			if converted := ConvertFromDescriptorReference(&reference); converted != nil {
				target.References[i] = *converted
			}
		}
	}

	return target
}

// ConvertToDescriptor converts a ComponentConstructor to a Descriptor.
// Returns nil if the input is nil or has no components.
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

// ConvertToDescriptorSourceRefs converts runtime SourceRefs to descriptor SourceRefs.
// Returns nil if the input slice is nil.
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

// ConvertFromDescriptorSourceRefs converts descriptor SourceRefs to runtime SourceRefs.
// Returns nil if the input slice is nil.
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
