package v1

import (
	"maps"
	"time"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ConvertToRuntimeResource converts Resource's to internal representation.
func ConvertToRuntimeResource(resource Resource) descriptor.Resource {
	var target descriptor.Resource
	target.Name = resource.Name
	target.Version = resource.Version
	target.Type = resource.Type
	target.CreationTime = descriptor.CreationTime(time.Now())
	if resource.Labels != nil {
		target.Labels = ConvertFromLabels(resource.Labels)
	}
	if resource.SourceRefs != nil {
		target.SourceRefs = ConvertFromSourceRefs(resource.SourceRefs)
	}
	if resource.Access != nil {
		target.Access = resource.Access.DeepCopy()
	}
	if resource.ExtraIdentity != nil {
		target.ExtraIdentity = resource.ExtraIdentity.DeepCopy()
	}
	target.Relation = descriptor.ResourceRelation(resource.Relation)
	return target
}

// ConvertToRuntimeSource converts Source to internal representation.
func ConvertToRuntimeSource(source Source) descriptor.Source {
	var target descriptor.Source
	target.Name = source.Name
	target.Version = source.Version
	target.Type = source.Type
	if source.Labels != nil {
		target.Labels = ConvertFromLabels(source.Labels)
	}
	if source.Access != nil {
		target.Access = source.Access.DeepCopy()
	}
	if source.ExtraIdentity != nil {
		target.ExtraIdentity = source.ExtraIdentity.DeepCopy()
	}
	return target
}

// ConvertToRuntimeReference converts Reference to internal representation.
func ConvertToRuntimeReference(reference Reference) descriptor.Reference {
	var target descriptor.Reference
	target.Name = reference.Name
	target.Version = reference.Version
	target.Component = reference.Component
	if reference.Labels != nil {
		target.Labels = ConvertFromLabels(reference.Labels)
	}
	if reference.ExtraIdentity != nil {
		target.ExtraIdentity = reference.ExtraIdentity.DeepCopy()
	}
	return target
}

// ConvertToRuntimeComponent converts Component to internal representation.
func ConvertToRuntimeComponent(component Component) descriptor.Component {
	var target descriptor.Component
	target.Name = component.Name
	target.Version = component.Version
	target.CreationTime = component.CreationTime
	if component.Labels != nil {
		target.Labels = ConvertFromLabels(component.Labels)
	}
	target.Provider = make(runtime.Identity)
	if component.Provider.Name != "" {
		target.Provider[IdentityAttributeName] = component.Provider.Name
	}
	if component.Provider.Labels != nil {
		for _, label := range component.Provider.Labels {
			target.Provider[label.Name] = label.Value
		}
	}
	if component.Resources != nil {
		target.Resources = make([]descriptor.Resource, len(component.Resources))
		for i, resource := range component.Resources {
			target.Resources[i] = ConvertToRuntimeResource(resource)
		}
	}
	if component.Sources != nil {
		target.Sources = make([]descriptor.Source, len(component.Sources))
		for i, source := range component.Sources {
			target.Sources[i] = ConvertToRuntimeSource(source)
		}
	}
	if component.References != nil {
		target.References = make([]descriptor.Reference, len(component.References))
		for i, reference := range component.References {
			target.References[i] = ConvertToRuntimeReference(reference)
		}
	}
	return target
}

// ConvertToRuntimeDescriptor converts ComponentConstructor to internal representation.
func ConvertToRuntimeDescriptor(constructor ComponentConstructor) *descriptor.Descriptor {
	if len(constructor.Components) == 0 {
		return nil
	}
	component := ConvertToRuntimeComponent(constructor.Components[0])
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v1",
		},
		Component: component,
	}
}

// ConvertFromLabels converts a list of Label to internal Label.
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

// ConvertFromSourceRefs converts v2 source references to internal format.
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
