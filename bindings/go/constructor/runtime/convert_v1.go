package runtime

import (
	"maps"

	v1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ConvertToRuntimeResource converts a Resource to its runtime representation.
func ConvertToRuntimeResource(resource *v1.Resource) descriptor.Resource {
	if resource == nil {
		return descriptor.Resource{}
	}

	target := descriptor.Resource{
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
		target.Labels = make([]descriptor.Label, len(resource.Labels))
		for i, label := range resource.Labels {
			target.Labels[i] = descriptor.Label{
				Name:    label.Name,
				Value:   label.Value,
				Signing: label.Signing,
			}
		}
	}

	if resource.SourceRefs != nil {
		target.SourceRefs = make([]descriptor.SourceRef, len(resource.SourceRefs))
		for i, ref := range resource.SourceRefs {
			target.SourceRefs[i] = descriptor.SourceRef{
				IdentitySelector: maps.Clone(ref.IdentitySelector),
				Labels:           make([]descriptor.Label, len(ref.Labels)),
			}
			for j, label := range ref.Labels {
				target.SourceRefs[i].Labels[j] = descriptor.Label{
					Name:    label.Name,
					Value:   label.Value,
					Signing: label.Signing,
				}
			}
		}
	}

	// Handle AccessOrInput - if Input is present, use that as Access
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

// ConvertToRuntimeSource converts a Source to its runtime representation.
func ConvertToRuntimeSource(source *v1.Source) descriptor.Source {
	if source == nil {
		return descriptor.Source{}
	}

	target := descriptor.Source{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    source.Name,
				Version: source.Version,
			},
		},
		Type: source.Type,
	}

	if source.Labels != nil {
		target.Labels = make([]descriptor.Label, len(source.Labels))
		for i, label := range source.Labels {
			target.Labels[i] = descriptor.Label{
				Name:    label.Name,
				Value:   label.Value,
				Signing: label.Signing,
			}
		}
	}

	// Handle AccessOrInput - if Input is present, use that as Access
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

// ConvertToRuntimeReference converts a Reference to its runtime representation.
func ConvertToRuntimeReference(reference *v1.Reference) descriptor.Reference {
	if reference == nil {
		return descriptor.Reference{}
	}

	target := descriptor.Reference{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    reference.Name,
				Version: reference.Version,
			},
		},
		Component: reference.Component,
	}

	if reference.Labels != nil {
		target.Labels = make([]descriptor.Label, len(reference.Labels))
		for i, label := range reference.Labels {
			target.Labels[i] = descriptor.Label{
				Name:    label.Name,
				Value:   label.Value,
				Signing: label.Signing,
			}
		}
	}

	if reference.ExtraIdentity != nil {
		target.ExtraIdentity = reference.ExtraIdentity.DeepCopy()
	}

	return target
}

// ConvertToRuntimeComponent converts a Component to its runtime representation.
func ConvertToRuntimeComponent(component *v1.Component) descriptor.Component {
	if component == nil {
		return descriptor.Component{}
	}

	target := descriptor.Component{
		ComponentMeta: descriptor.ComponentMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    component.Name,
				Version: component.Version,
			},
			CreationTime: component.CreationTime,
		},
		Provider: make(runtime.Identity),
	}

	if component.Labels != nil {
		target.Labels = make([]descriptor.Label, len(component.Labels))
		for i, label := range component.Labels {
			target.Labels[i] = descriptor.Label{
				Name:    label.Name,
				Value:   label.Value,
				Signing: label.Signing,
			}
		}
	}

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

// ConvertToRuntimeDescriptor converts a ComponentConstructor to its runtime representation.
func ConvertToRuntimeDescriptor(constructor *v1.ComponentConstructor) *descriptor.Descriptor {
	if constructor == nil {
		return nil
	}

	if len(constructor.Components) == 0 {
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

// ConvertToRuntimeConstructor converts a ComponentConstructor to its runtime representation.
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
				ObjectMeta: ObjectMeta{
					Name:    component.Name,
					Version: component.Version,
				},
				CreationTime: component.CreationTime,
			},
			Provider: Provider{
				Name:   component.Provider.Name,
				Labels: make([]Label, len(component.Provider.Labels)),
			},
		}

		// Copy provider labels
		for j, label := range component.Provider.Labels {
			target.Components[i].Provider.Labels[j] = Label{
				Name:    label.Name,
				Value:   label.Value,
				Signing: label.Signing,
			}
		}

		// Copy component labels
		if component.Labels != nil {
			target.Components[i].Labels = make([]Label, len(component.Labels))
			for j, label := range component.Labels {
				target.Components[i].Labels[j] = Label{
					Name:    label.Name,
					Value:   label.Value,
					Signing: label.Signing,
				}
			}
		}

		// Copy resources
		if component.Resources != nil {
			target.Components[i].Resources = make([]Resource, len(component.Resources))
			for j, resource := range component.Resources {
				target.Components[i].Resources[j] = Resource{
					ElementMeta: ElementMeta{
						ObjectMeta: ObjectMeta{
							Name:    resource.Name,
							Version: resource.Version,
						},
					},
					Type:     resource.Type,
					Relation: ResourceRelation(resource.Relation),
				}

				// Copy resource labels
				if resource.Labels != nil {
					target.Components[i].Resources[j].Labels = make([]Label, len(resource.Labels))
					for k, label := range resource.Labels {
						target.Components[i].Resources[j].Labels[k] = Label{
							Name:    label.Name,
							Value:   label.Value,
							Signing: label.Signing,
						}
					}
				}

				// Copy resource access or input
				if resource.AccessOrInput.HasInput() {
					target.Components[i].Resources[j].AccessOrInput.Input = resource.AccessOrInput.Input.DeepCopyTyped()
				} else if resource.AccessOrInput.HasAccess() {
					target.Components[i].Resources[j].AccessOrInput.Access = resource.AccessOrInput.Access.DeepCopyTyped()
				}
			}
		}

		// Copy sources
		if component.Sources != nil {
			target.Components[i].Sources = make([]Source, len(component.Sources))
			for j, source := range component.Sources {
				target.Components[i].Sources[j] = Source{
					ElementMeta: ElementMeta{
						ObjectMeta: ObjectMeta{
							Name:    source.Name,
							Version: source.Version,
						},
					},
					Type: source.Type,
				}

				// Copy source labels
				if source.Labels != nil {
					target.Components[i].Sources[j].Labels = make([]Label, len(source.Labels))
					for k, label := range source.Labels {
						target.Components[i].Sources[j].Labels[k] = Label{
							Name:    label.Name,
							Value:   label.Value,
							Signing: label.Signing,
						}
					}
				}

				// Copy source access or input
				if source.AccessOrInput.HasInput() {
					target.Components[i].Sources[j].AccessOrInput.Input = source.AccessOrInput.Input.DeepCopyTyped()
				} else if source.AccessOrInput.HasAccess() {
					target.Components[i].Sources[j].AccessOrInput.Access = source.AccessOrInput.Access.DeepCopyTyped()
				}
			}
		}

		// Copy references
		if component.References != nil {
			target.Components[i].References = make([]Reference, len(component.References))
			for j, reference := range component.References {
				target.Components[i].References[j] = Reference{
					ElementMeta: ElementMeta{
						ObjectMeta: ObjectMeta{
							Name:    reference.Name,
							Version: reference.Version,
						},
					},
					Component: reference.Component,
				}

				// Copy reference labels
				if reference.Labels != nil {
					target.Components[i].References[j].Labels = make([]Label, len(reference.Labels))
					for k, label := range reference.Labels {
						target.Components[i].References[j].Labels[k] = Label{
							Name:    label.Name,
							Value:   label.Value,
							Signing: label.Signing,
						}
					}
				}
			}
		}
	}

	return target
}
