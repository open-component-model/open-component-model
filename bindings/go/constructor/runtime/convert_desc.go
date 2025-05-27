package runtime

import (
	"maps"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ConvertToDescriptorResource converts Resource to descriptor representation.
func ConvertToDescriptorResource(resource *Resource) *descriptor.Resource {
	if resource == nil {
		return nil
	}
	target := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    resource.Name,
				Version: resource.Version,
			},
		},
		Type:     resource.Type,
		Relation: descriptor.ResourceRelation(resource.Relation),
	}

	if resource.Labels != nil {
		target.Labels = ConvertFromLabels(resource.Labels)
	}
	if resource.SourceRefs != nil {
		target.SourceRefs = ConvertFromSourceRefs(resource.SourceRefs)
	}
	if resource.AccessOrInput.HasInput() {
		target.Access = resource.AccessOrInput.Input.DeepCopyTyped()
	} else if resource.AccessOrInput.HasAccess() {
		target.Access = resource.AccessOrInput.Access.DeepCopyTyped()
	}
	if resource.ExtraIdentity != nil {
		target.ExtraIdentity = resource.ExtraIdentity.DeepCopy()
	}
	return target
}

// ConvertToDescriptorSource converts Source to descriptor representation.
func ConvertToDescriptorSource(source *Source) *descriptor.Source {
	if source == nil {
		return nil
	}
	target := &descriptor.Source{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    source.Name,
				Version: source.Version,
			},
		},
		Type: source.Type,
	}

	if source.Labels != nil {
		target.Labels = ConvertFromLabels(source.Labels)
	}
	if source.AccessOrInput.HasInput() {
		target.Access = source.AccessOrInput.Input.DeepCopyTyped()
	} else if source.AccessOrInput.HasAccess() {
		target.Access = source.AccessOrInput.Access.DeepCopyTyped()
	}
	if source.ExtraIdentity != nil {
		target.ExtraIdentity = source.ExtraIdentity.DeepCopy()
	}
	return target
}

// ConvertToDescriptorReference converts Reference to descriptor representation.
func ConvertToDescriptorReference(reference *Reference) *descriptor.Reference {
	if reference == nil {
		return nil
	}
	target := &descriptor.Reference{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    reference.Name,
				Version: reference.Version,
			},
		},
		Component: reference.Component,
	}

	if reference.Labels != nil {
		target.Labels = ConvertFromLabels(reference.Labels)
	}
	if reference.ExtraIdentity != nil {
		target.ExtraIdentity = reference.ExtraIdentity.DeepCopy()
	}
	return target
}

// ConvertToDescriptorComponent converts Component to descriptor representation.
func ConvertToDescriptorComponent(component *Component) *descriptor.Component {
	if component == nil {
		return nil
	}
	target := &descriptor.Component{
		ComponentMeta: descriptor.ComponentMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    component.Name,
				Version: component.Version,
			},
		},
	}

	if component.Labels != nil {
		target.Labels = ConvertFromLabels(component.Labels)
	}

	// Convert provider to runtime.Identity
	target.Provider = make(runtime.Identity)
	if component.Provider.Name != "" {
		target.Provider["name"] = component.Provider.Name
	}
	if component.Provider.Labels != nil {
		for _, label := range component.Provider.Labels {
			target.Provider[label.Name] = label.Value
		}
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

// ConvertToDescriptor converts ComponentConstructor to descriptor representation.
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

// ConvertFromLabels converts a list of Label to descriptor Label.
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

// ConvertFromSourceRefs converts source references to descriptor format.
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
