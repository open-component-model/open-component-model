package spec

import (
	"maps"
	"time"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

//
// func ConvertToRuntimeResource(resource *Resource) (*descriptor.Resource, error) {
// 	descriptor.Resource{
// 		ElementMeta: descriptor.ElementMeta{
// 			ObjectMeta: descriptor.ObjectMeta{
// 				Name:    "",
// 				Version: "",
// 				Labels:  nil,
// 			},
// 			ExtraIdentity: nil,
// 		},
// 		SourceRefs:   nil,
// 		Type:         "",
// 		Relation:     "",
// 		Access:       nil,
// 		Digest:       nil,
// 		Size:         0,
// 		CreationTime: descriptor.CreationTime{},
// 	}
// }

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
