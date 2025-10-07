package runtime

import (
	"maps"

	v1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Label conversion functions

// ConvertFromV1Labels converts a slice of v1 Labels to runtime Labels.
// Returns nil if the input slice is nil.
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

// ConvertToV1Labels converts a slice of runtime Labels to v1 Labels.
// Returns nil if the input slice is nil.
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

// ConvertFromV1ObjectMeta converts v1 ObjectMeta to runtime ObjectMeta.
func ConvertFromV1ObjectMeta(meta v1.ObjectMeta) ObjectMeta {
	return ObjectMeta{
		Name:    meta.Name,
		Version: meta.Version,
		Labels:  ConvertFromV1Labels(meta.Labels),
	}
}

// ConvertToV1ObjectMeta converts runtime ObjectMeta to v1 ObjectMeta.
func ConvertToV1ObjectMeta(meta ObjectMeta) v1.ObjectMeta {
	return v1.ObjectMeta{
		Name:    meta.Name,
		Version: meta.Version,
		Labels:  ConvertToV1Labels(meta.Labels),
	}
}

// ConvertFromV1ElementMeta converts v1 ElementMeta to runtime ElementMeta.
func ConvertFromV1ElementMeta(meta v1.ElementMeta) ElementMeta {
	return ElementMeta{
		ObjectMeta:    ConvertFromV1ObjectMeta(meta.ObjectMeta),
		ExtraIdentity: meta.ExtraIdentity.DeepCopy(),
	}
}

// ConvertToV1ElementMeta converts runtime ElementMeta to v1 ElementMeta.
func ConvertToV1ElementMeta(meta ElementMeta) v1.ElementMeta {
	return v1.ElementMeta{
		ObjectMeta:    ConvertToV1ObjectMeta(meta.ObjectMeta),
		ExtraIdentity: meta.ExtraIdentity.DeepCopy(),
	}
}

// Resource conversion

// ConvertFromV1Resource converts a v1 Resource to runtime Resource.
// Returns an empty Resource if the input is nil.
func ConvertFromV1Resource(resource *v1.Resource) Resource {
	if resource == nil {
		return Resource{}
	}

	target := Resource{
		ElementMeta: ConvertFromV1ElementMeta(resource.ElementMeta),
		Type:        resource.Type,
		Relation:    ResourceRelation(resource.Relation),
		ConstructorAttributes: ConstructorAttributes{
			CopyPolicy: CopyPolicy(resource.CopyPolicy),
		},
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

// ConvertToV1Resource converts a runtime Resource to v1 Resource.
// Returns nil and no error if the input is nil.
func ConvertToV1Resource(resource *Resource) (*v1.Resource, error) {
	if resource == nil {
		return nil, nil
	}

	target := v1.Resource{
		ElementMeta: ConvertToV1ElementMeta(resource.ElementMeta),
		Type:        resource.Type,
		Relation:    v1.ResourceRelation(resource.Relation),
		ConstructorAttributes: v1.ConstructorAttributes{
			CopyPolicy: v1.CopyPolicy(resource.CopyPolicy),
		},
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

	if resource.HasInput() {
		var raw runtime.Raw
		if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(resource.Input, &raw); err != nil {
			return nil, err
		}
		target.Input = &raw
	} else if resource.HasAccess() {
		var raw runtime.Raw
		if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(resource.Access, &raw); err != nil {
			return nil, err
		}
		target.Access = &raw
	}

	return &target, nil
}

// Source conversion

// ConvertFromV1Source converts a v1 Source to runtime Source.
// Returns an empty Source if the input is nil.
func ConvertFromV1Source(source *v1.Source) Source {
	if source == nil {
		return Source{}
	}

	target := Source{
		ElementMeta: ConvertFromV1ElementMeta(source.ElementMeta),
		Type:        source.Type,
		ConstructorAttributes: ConstructorAttributes{
			CopyPolicy: CopyPolicy(source.CopyPolicy),
		},
	}

	if source.Access != nil {
		target.Access = source.Access.DeepCopy()
	}
	if source.Input != nil {
		target.Input = source.Input.DeepCopy()
	}

	return target
}

// ConvertToV1Source converts a runtime Source to v1 Source.
// Returns nil and no error if the input is nil.
func ConvertToV1Source(source *Source) (*v1.Source, error) {
	if source == nil {
		return nil, nil
	}

	target := v1.Source{
		ElementMeta: ConvertToV1ElementMeta(source.ElementMeta),
		Type:        source.Type,
		ConstructorAttributes: v1.ConstructorAttributes{
			CopyPolicy: v1.CopyPolicy(source.CopyPolicy),
		},
	}

	if source.HasInput() {
		var raw runtime.Raw
		if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(source.Input, &raw); err != nil {
			return nil, err
		}
		target.Input = &raw
	} else if source.HasAccess() {
		var raw runtime.Raw
		if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(source.Access, &raw); err != nil {
			return nil, err
		}
		target.Access = &raw
	}

	return &target, nil
}

// Reference conversion

// ConvertToRuntimeReference converts a v1 Reference to runtime Reference.
// Returns an empty Reference if the input is nil.
// Note: This conversion is lossy as it does not include the digest field.
func ConvertToRuntimeReference(reference *v1.Reference) Reference {
	if reference == nil {
		return Reference{}
	}

	return Reference{
		ElementMeta: ConvertFromV1ElementMeta(reference.ElementMeta),
		Component:   reference.Component,
	}
}

// Component conversion

// ConvertToRuntimeComponent converts a v1 Component to runtime Component.
// Returns an empty Component if the input is nil.
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

// ConvertToRuntimeConstructorResource converts a v1 Resource to runtime Resource
// for use in a ComponentConstructor.
func ConvertToRuntimeConstructorResource(resource v1.Resource) Resource {
	target := Resource{
		ElementMeta: ConvertFromV1ElementMeta(resource.ElementMeta),
		Type:        resource.Type,
		Relation:    ResourceRelation(resource.Relation),
		ConstructorAttributes: ConstructorAttributes{
			CopyPolicy: CopyPolicy(resource.CopyPolicy),
		},
	}

	if resource.HasInput() {
		target.Input = resource.Input.DeepCopyTyped()
	} else if resource.HasAccess() {
		target.Access = resource.Access.DeepCopyTyped()
	}

	return target
}

// ConvertToRuntimeConstructorSource converts a v1 Source to runtime Source
// for use in a ComponentConstructor.
func ConvertToRuntimeConstructorSource(source v1.Source) Source {
	target := Source{
		ElementMeta: ConvertFromV1ElementMeta(source.ElementMeta),
		Type:        source.Type,
		ConstructorAttributes: ConstructorAttributes{
			CopyPolicy: CopyPolicy(source.CopyPolicy),
		},
	}

	if source.HasInput() {
		target.Input = source.Input.DeepCopyTyped()
	} else if source.HasAccess() {
		target.Access = source.Access.DeepCopyTyped()
	}

	return target
}

// ConvertToRuntimeConstructorReference converts a v1 Reference to runtime Reference
// for use in a ComponentConstructor.
func ConvertToRuntimeConstructorReference(reference v1.Reference) Reference {
	return Reference{
		ElementMeta: ConvertFromV1ElementMeta(reference.ElementMeta),
		Component:   reference.Component,
	}
}

// ConvertToRuntimeConstructor converts a v1 ComponentConstructor to runtime ComponentConstructor.
// Returns nil if the input is nil.
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
				target.Components[i].Resources[j] = ConvertToRuntimeConstructorResource(resource)
			}
		}

		// Copy sources
		if component.Sources != nil {
			target.Components[i].Sources = make([]Source, len(component.Sources))
			for j, source := range component.Sources {
				target.Components[i].Sources[j] = ConvertToRuntimeConstructorSource(source)
			}
		}

		// Copy references
		if component.References != nil {
			target.Components[i].References = make([]Reference, len(component.References))
			for j, reference := range component.References {
				target.Components[i].References[j] = ConvertToRuntimeConstructorReference(reference)
			}
		}
	}

	return target
}

// ConvertToV1Component converts a runtime Component to v1 Component.
// Returns nil if the input is nil.
func ConvertToV1Component(component *Component) (*v1.Component, error) {
	if component == nil {
		return nil, nil
	}

	target := v1.Component{
		ComponentMeta: v1.ComponentMeta{
			ObjectMeta:   ConvertToV1ObjectMeta(component.ObjectMeta),
			CreationTime: component.CreationTime,
		},
		Provider: v1.Provider{
			Name:   component.Provider.Name,
			Labels: ConvertToV1Labels(component.Provider.Labels),
		},
	}

	if component.Resources != nil {
		target.Resources = make([]v1.Resource, len(component.Resources))
		for i, resource := range component.Resources {
			v1Resource, err := ConvertToV1Resource(&resource)
			if err != nil {
				return nil, err
			}
			target.Resources[i] = *v1Resource
		}
	}

	if component.Sources != nil {
		target.Sources = make([]v1.Source, len(component.Sources))
		for i, source := range component.Sources {
			v1Source, err := ConvertToV1Source(&source)
			if err != nil {
				return nil, err
			}
			target.Sources[i] = *v1Source
		}
	}

	if component.References != nil {
		target.References = make([]v1.Reference, len(component.References))
		for i, reference := range component.References {
			v1Reference, err := ConvertToV1Reference(&reference)
			if err != nil {
				return nil, err
			}
			target.References[i] = *v1Reference
		}
	}

	return &target, nil
}

// ConvertToV1Reference converts a runtime Reference to v1 Reference.
// Returns nil if the input is nil.
func ConvertToV1Reference(reference *Reference) (*v1.Reference, error) {
	if reference == nil {
		return nil, nil
	}

	target := v1.Reference{
		ElementMeta: ConvertToV1ElementMeta(reference.ElementMeta),
		Component:   reference.Component,
	}

	return &target, nil
}
