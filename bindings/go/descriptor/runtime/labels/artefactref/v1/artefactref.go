// Package v1 implements the "ocm.software/artefact-references" label
// https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/06-conventions.md#artefact-linking-label
//
//	resources:
//	  - name: my-image
//	    version: 1.2.3
//	    type: ociImage
//	    extraIdentity:
//	      foo: bar
//	  - name: my-image-sbom
//	    version: 1.2.3
//	    type: sbom
//	    extraIdentity:
//	      architecture: amd64
//	    labels:
//	      - name: ocm.software/artefact-references
//	        version: v1
//	        value:
//	          identity:
//	            name: my-image
//	            version: 1.2.3
//	            foo: bar
package v1

import (
	"errors"
	"fmt"
	"maps"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrNotFound used to signal that there are no references.
var ErrNotFound = errors.New("no resource references the artefact")

const (
	// LabelName is the name under which artefact references are stored on an element.
	LabelName = "ocm.software/artefact-references"
	// Version is the specification version of the label value implemented here.
	Version = "v1"
)

// Reference is the decoded value of the artefact references label. Its Identity selects
// the artefact being described.
type Reference struct {
	Identity runtime.Identity `json:"identity"`
}

// Describes checks whether the reference points to the subject artefact. The following
// rules are used for matches as described by the spec:
//
//   - name is required and MUST be equal.
//   - version is optional; when given it MUST be equal, otherwise any version matches.
//   - every remaining attribute is an extra identity property, and those MUST match the
//     subject's extra identity exactly, and back.
func (r Reference) Describes(subject runtime.Identity) bool {
	name, ok := r.Identity[descriptor.IdentityAttributeName]
	if !ok || name == "" || name != subject[descriptor.IdentityAttributeName] {
		return false
	}
	if version, ok := r.Identity[descriptor.IdentityAttributeVersion]; ok &&
		version != subject[descriptor.IdentityAttributeVersion] {
		return false
	}
	return maps.Equal(extraIdentityOf(r.Identity), extraIdentityOf(subject))
}

// extraIdentityOf returns the identity without the attributes.
func extraIdentityOf(identity runtime.Identity) runtime.Identity {
	extra := make(runtime.Identity, len(identity))
	for key, value := range identity {
		if key == descriptor.IdentityAttributeName || key == descriptor.IdentityAttributeVersion {
			continue
		}
		extra[key] = value
	}
	return extra
}

// FromLabels decodes the artefact reference label out of labels.
func FromLabels(labels []descriptor.Label) (Reference, bool, error) {
	for _, label := range labels {
		if label.Name != LabelName {
			continue
		}
		if label.Version != "" && label.Version != Version {
			continue
		}
		var ref Reference
		if err := label.GetValue(&ref); err != nil {
			return Reference{}, false, fmt.Errorf("interpreting label %q failed: %w", LabelName, err)
		}
		return ref, true, nil
	}
	return Reference{}, false, nil
}

// FindDescribingResources returns every resource in desc whose artefact reference label
// selects the target, ordered by appearance.
func FindDescribingResources(desc *descriptor.Descriptor, target runtime.Identity) ([]*descriptor.Resource, error) {
	var matches []*descriptor.Resource
	for i := range desc.Component.Resources {
		resource := &desc.Component.Resources[i]
		ref, ok, err := FromLabels(resource.Labels)
		if err != nil {
			return nil, fmt.Errorf("resource %q: %w", resource.ToIdentity(), err)
		}
		if !ok || !ref.Describes(target) {
			continue
		}
		matches = append(matches, resource)
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, target)
	}
	return matches, nil
}
