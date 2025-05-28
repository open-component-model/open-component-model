package runtime

import (
	"maps"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Label conversion functions

func ConvertFromLabels(labels []Label) []descriptor.Label {
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

// Common conversion helpers

func convertObjectMetaToDescriptor(meta ObjectMeta) descriptor.ObjectMeta {
	return descriptor.ObjectMeta{
		Name:    meta.Name,
		Version: meta.Version,
		Labels:  ConvertFromLabels(meta.Labels),
	}
}

func convertElementMetaToDescriptor(meta ElementMeta) descriptor.ElementMeta {
	return descriptor.ElementMeta{
		ObjectMeta:    convertObjectMetaToDescriptor(meta.ObjectMeta),
		ExtraIdentity: meta.ExtraIdentity.DeepCopy(),
	}
}

func handleAccessOrInputToDescriptor(accessOrInput AccessOrInput) runtime.Typed {
	if accessOrInput.HasInput() {
		return accessOrInput.Input.DeepCopyTyped()
	} else if accessOrInput.HasAccess() {
		return accessOrInput.Access.DeepCopyTyped()
	}
	return nil
}

// Resource conversion

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
		target.SourceRefs = ConvertFromSourceRefs(resource.SourceRefs)
	}

	target.Access = handleAccessOrInputToDescriptor(resource.AccessOrInput)
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
		Access:      handleAccessOrInputToDescriptor(source.AccessOrInput),
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
func convertProviderToDescriptor(provider Provider) (runtime.Identity, error) {
	if provider.Name == "" && len(provider.Labels) == 0 {
		return nil, nil
	}
	if len(provider.Labels) == 0 {
		return runtime.Identity{
			IdentityAttributeName: provider.Name,
		}, nil
	}
	// If we have labels, create a full identity map
	target := make(runtime.Identity)
	if provider.Name != "" {
		target[IdentityAttributeName] = provider.Name
	}
	for _, label := range provider.Labels {
		target[label.Name] = label.Value
	}
	return target, nil
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

func ConvertFromSourceRefs(refs []SourceRef) []descriptor.SourceRef {
	if refs == nil {
		return nil
	}
	n := make([]descriptor.SourceRef, len(refs))
	for i := range refs {
		n[i].IdentitySelector = maps.Clone(refs[i].IdentitySelector)
		n[i].Labels = ConvertFromLabels(refs[i].Labels)
	}
	return n
}
