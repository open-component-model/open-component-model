package runtime

import (
	"maps"

	v1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Label conversion functions

func ConvertFromV1Labels(labels []v1.Label) []Label {
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

func ConvertToV1Labels(labels []Label) []v1.Label {
	if labels == nil {
		return nil
	}
	result := make([]v1.Label, len(labels))
	for i, label := range labels {
		result[i] = v1.Label{
			Name:    label.Name,
			Value:   label.Value,
			Signing: label.Signing,
		}
	}
	return result
}

// Common conversion helpers

func ConvertFromV1ObjectMeta(meta v1.ObjectMeta) ObjectMeta {
	return ObjectMeta{
		Name:    meta.Name,
		Version: meta.Version,
		Labels:  ConvertFromV1Labels(meta.Labels),
	}
}

func ConvertToV1ObjectMeta(meta ObjectMeta) v1.ObjectMeta {
	return v1.ObjectMeta{
		Name:    meta.Name,
		Version: meta.Version,
		Labels:  ConvertToV1Labels(meta.Labels),
	}
}

func ConvertFromV1ElementMeta(meta v1.ElementMeta) ElementMeta {
	return ElementMeta{
		ObjectMeta:    ConvertFromV1ObjectMeta(meta.ObjectMeta),
		ExtraIdentity: meta.ExtraIdentity.DeepCopy(),
	}
}

func ConvertToV1ElementMeta(meta ElementMeta) v1.ElementMeta {
	return v1.ElementMeta{
		ObjectMeta:    ConvertToV1ObjectMeta(meta.ObjectMeta),
		ExtraIdentity: meta.ExtraIdentity.DeepCopy(),
	}
}

// Resource conversion

func ConvertFromV1Resource(resource *v1.Resource) Resource {
	if resource == nil {
		return Resource{}
	}

	target := Resource{
		ElementMeta: ConvertFromV1ElementMeta(resource.ElementMeta),
		Type:        resource.Type,
		Relation:    ResourceRelation(resource.Relation),
	}

	if resource.SourceRefs != nil {
		target.SourceRefs = make([]SourceRef, len(resource.SourceRefs))
		for i, ref := range resource.SourceRefs {
			target.SourceRefs[i] = SourceRef{
				IdentitySelector: maps.Clone(ref.IdentitySelector),
				Labels:           ConvertFromV1Labels(ref.Labels),
			}
		}
	}

	if resource.Access != nil {
		target.Access = resource.Access.DeepCopy()
	}
	if resource.Input != nil {
		target.Input = resource.Input.DeepCopy()
	}

	return target
}

func ConvertToV1Resource(resource *Resource) (*v1.Resource, error) {
	if resource == nil {
		return nil, nil
	}

	target := v1.Resource{
		ElementMeta: ConvertToV1ElementMeta(resource.ElementMeta),
		Type:        resource.Type,
		Relation:    v1.ResourceRelation(resource.Relation),
	}

	if resource.SourceRefs != nil {
		target.SourceRefs = make([]v1.SourceRef, len(resource.SourceRefs))
		for i, ref := range resource.SourceRefs {
			target.SourceRefs[i] = v1.SourceRef{
				IdentitySelector: maps.Clone(ref.IdentitySelector),
				Labels:           ConvertToV1Labels(ref.Labels),
			}
		}
	}

	if resource.HasAccess() {
		var raw runtime.Raw
		if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(resource.Access, &raw); err != nil {
			return nil, err
		}
		target.Access = &raw
	}

	if resource.HasInput() {
		var raw runtime.Raw
		if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(resource.Input, &raw); err != nil {
			return nil, err
		}
		target.Input = &raw
	}

	return &target, nil
}

// Source conversion

func ConvertFromV1Source(source *v1.Source) Source {
	if source == nil {
		return Source{}
	}

	target := Source{
		ElementMeta: ConvertFromV1ElementMeta(source.ElementMeta),
		Type:        source.Type,
	}

	if source.Access != nil {
		target.Access = source.Access.DeepCopy()
	}

	return target
}

func ConvertToV1Source(source *Source) (*v1.Source, error) {
	if source == nil {
		return nil, nil
	}

	target := v1.Source{
		ElementMeta: ConvertToV1ElementMeta(source.ElementMeta),
		Type:        source.Type,
	}

	if source.HasAccess() {
		var raw runtime.Raw
		if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(source.Access, &raw); err != nil {
			return nil, err
		}
		target.Access = &raw
	}

	if source.HasInput() {
		var raw runtime.Raw
		if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(source.Input, &raw); err != nil {
			return nil, err
		}
		target.Input = &raw
	}

	return &target, nil
}

// Reference conversion

func ConvertToRuntimeReference(reference *v1.Reference) Reference {
	if reference == nil {
		return Reference{}
	}

	target := Reference{
		ElementMeta: ConvertFromV1ElementMeta(reference.ElementMeta),
		Component:   reference.Component,
	}

	return target
}

// Component conversion

func ConvertToRuntimeComponent(component *v1.Component) Component {
	if component == nil {
		return Component{}
	}

	target := Component{
		ComponentMeta: ComponentMeta{
			ObjectMeta:   ConvertFromV1ObjectMeta(component.ObjectMeta),
			CreationTime: component.CreationTime,
		},
		Provider: Provider{},
	}

	if component.Provider.Name != "" {
		target.Provider.Name = component.Provider.Name
	}
	if component.Provider.Labels != nil {
		target.Provider.Labels = ConvertFromV1Labels(component.Provider.Labels)
	}

	if component.Resources != nil {
		target.Resources = make([]Resource, len(component.Resources))
		for i, resource := range component.Resources {
			target.Resources[i] = ConvertFromV1Resource(&resource)
		}
	}

	if component.Sources != nil {
		target.Sources = make([]Source, len(component.Sources))
		for i, source := range component.Sources {
			target.Sources[i] = ConvertFromV1Source(&source)
		}
	}

	if component.References != nil {
		target.References = make([]Reference, len(component.References))
		for i, reference := range component.References {
			target.References[i] = ConvertToRuntimeReference(&reference)
		}
	}

	return target
}

// Constructor conversion

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
				Labels: ConvertFromV1Labels(component.Provider.Labels),
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
