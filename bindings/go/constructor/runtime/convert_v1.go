package runtime

import (
	"maps"

	v1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Label conversion functions

func ConvertV1LabelsToDescriptorLabels(labels []v1.Label) []descriptor.Label {
	if labels == nil {
		return nil
	}
	result := make([]descriptor.Label, len(labels))
	for i, label := range labels {
		result[i] = descriptor.Label{
			Name:    label.Name,
			Value:   label.Value,
			Signing: label.Signing,
		}
	}
	return result
}

func ConvertV1LabelsToLabels(labels []v1.Label) []Label {
	if labels == nil {
		return nil
	}
	result := make([]Label, len(labels))
	for i, label := range labels {
		result[i] = Label{
			Name:    label.Name,
			Value:   label.Value,
			Signing: label.Signing,
		}
	}
	return result
}

// Common conversion helpers

func ConvertObjectMeta(meta v1.ObjectMeta) descriptor.ObjectMeta {
	return descriptor.ObjectMeta{
		Name:    meta.Name,
		Version: meta.Version,
		Labels:  ConvertV1LabelsToDescriptorLabels(meta.Labels),
	}
}

func ConvertElementMeta(meta v1.ElementMeta) descriptor.ElementMeta {
	return descriptor.ElementMeta{
		ObjectMeta:    ConvertObjectMeta(meta.ObjectMeta),
		ExtraIdentity: meta.ExtraIdentity.DeepCopy(),
	}
}

// Resource conversion

func ConvertToRuntimeResource(resource *v1.Resource) descriptor.Resource {
	if resource == nil {
		return descriptor.Resource{}
	}

	target := descriptor.Resource{
		ElementMeta: ConvertElementMeta(resource.ElementMeta),
		Type:        resource.Type,
		Relation:    descriptor.ResourceRelation(resource.Relation),
	}

	if resource.SourceRefs != nil {
		target.SourceRefs = make([]descriptor.SourceRef, len(resource.SourceRefs))
		for i, ref := range resource.SourceRefs {
			target.SourceRefs[i] = descriptor.SourceRef{
				IdentitySelector: maps.Clone(ref.IdentitySelector),
				Labels:           ConvertV1LabelsToDescriptorLabels(ref.Labels),
			}
		}
	}

	if resource.Access != nil {
		target.Access = resource.Access.DeepCopy()
	}

	return target
}

// Source conversion

func ConvertToRuntimeSource(source *v1.Source) descriptor.Source {
	if source == nil {
		return descriptor.Source{}
	}

	target := descriptor.Source{
		ElementMeta: ConvertElementMeta(source.ElementMeta),
		Type:        source.Type,
	}

	if source.Access != nil {
		target.Access = source.Access.DeepCopy()
	}

	return target
}

// Reference conversion

func ConvertToRuntimeReference(reference *v1.Reference) descriptor.Reference {
	if reference == nil {
		return descriptor.Reference{}
	}

	target := descriptor.Reference{
		ElementMeta: ConvertElementMeta(reference.ElementMeta),
		Component:   reference.Component,
	}

	return target
}

// Component conversion

func ConvertToRuntimeComponent(component *v1.Component) descriptor.Component {
	if component == nil {
		return descriptor.Component{}
	}

	target := descriptor.Component{
		ComponentMeta: descriptor.ComponentMeta{
			ObjectMeta:   ConvertObjectMeta(component.ObjectMeta),
			CreationTime: component.CreationTime,
		},
		Provider: descriptor.Provider{},
	}

	if component.Provider.Name != "" {
		target.Provider.Name = component.Provider.Name
	}
	if component.Provider.Labels != nil {
		target.Provider.Labels = ConvertV1LabelsToDescriptorLabels(component.Provider.Labels)
	}

	if component.Resources != nil {
		target.Resources = make([]descriptor.Resource, len(component.Resources))
		for i, resource := range component.Resources {
			target.Resources[i] = ConvertToRuntimeResource(&resource)
		}
	}

	if component.Sources != nil {
		target.Sources = make([]descriptor.Source, len(component.Sources))
		for i, source := range component.Sources {
			target.Sources[i] = ConvertToRuntimeSource(&source)
		}
	}

	if component.References != nil {
		target.References = make([]descriptor.Reference, len(component.References))
		for i, reference := range component.References {
			target.References[i] = ConvertToRuntimeReference(&reference)
		}
	}

	return target
}

// Constructor conversion

func ConvertToRuntimeDescriptor(constructor *v1.ComponentConstructor) *descriptor.Descriptor {
	if constructor == nil || len(constructor.Components) == 0 {
		return nil
	}

	component := ConvertToRuntimeComponent(&constructor.Components[0])
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v1",
		},
		Component: component,
	}
}

// Runtime constructor resource conversion
func convertToRuntimeConstructorResource(resource v1.Resource) Resource {
	target := Resource{
		ElementMeta: ElementMeta{
			ObjectMeta: ObjectMeta{
				Name:    resource.Name,
				Version: resource.Version,
			},
		},
		Type:     resource.Type,
		Relation: ResourceRelation(resource.Relation),
	}

	if resource.HasInput() {
		target.Input = resource.Input.DeepCopyTyped()
	} else if resource.HasAccess() {
		target.Access = resource.Access.DeepCopyTyped()
	}

	return target
}

// Runtime constructor source conversion
func convertToRuntimeConstructorSource(source v1.Source) Source {
	target := Source{
		ElementMeta: ElementMeta{
			ObjectMeta: ObjectMeta{
				Name:    source.Name,
				Version: source.Version,
			},
		},
		Type: source.Type,
	}

	if source.HasInput() {
		target.Input = source.Input.DeepCopyTyped()
	} else if source.HasAccess() {
		target.Access = source.Access.DeepCopyTyped()
	}

	return target
}

// Runtime constructor reference conversion
func convertToRuntimeConstructorReference(reference v1.Reference) Reference {
	return Reference{
		ElementMeta: ElementMeta{
			ObjectMeta: ObjectMeta{
				Name:    reference.Name,
				Version: reference.Version,
			},
		},
		Component: reference.Component,
	}
}

func ConvertToRuntimeConstructor(constructor *v1.ComponentConstructor) *ComponentConstructor {
	if constructor == nil {
		return nil
	}

	target := &ComponentConstructor{
		Components: make([]Component, len(constructor.Components)),
	}

	for i, component := range constructor.Components {
		target.Components[i] = Component{
			ComponentMeta: ComponentMeta{
				ObjectMeta:   ObjectMeta{Name: component.Name, Version: component.Version},
				CreationTime: component.CreationTime,
			},
			Provider: Provider{
				Name:   component.Provider.Name,
				Labels: ConvertV1LabelsToLabels(component.Provider.Labels),
			},
		}

		// Copy resources
		if component.Resources != nil {
			target.Components[i].Resources = make([]Resource, len(component.Resources))
			for j, resource := range component.Resources {
				target.Components[i].Resources[j] = convertToRuntimeConstructorResource(resource)
			}
		}

		// Copy sources
		if component.Sources != nil {
			target.Components[i].Sources = make([]Source, len(component.Sources))
			for j, source := range component.Sources {
				target.Components[i].Sources[j] = convertToRuntimeConstructorSource(source)
			}
		}

		// Copy references
		if component.References != nil {
			target.Components[i].References = make([]Reference, len(component.References))
			for j, reference := range component.References {
				target.Components[i].References[j] = convertToRuntimeConstructorReference(reference)
			}
		}
	}

	return target
}
