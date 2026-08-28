// Package v1alpha1 implements the "ocm.software/artifact-references" label
// https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/06-conventions.md#artifact-linking-label
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
//	      - name: ocm.software/artifact-references
//	        version: v1alpha1
//	        value:
//	          - identity:
//	              name: my-image
//	              version: 1.2.3
//	              foo: bar
package v1alpha1

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrNotFound used to signal that there are no references.
var ErrNotFound = errors.New("no resource references the artifact")

const (
	// LabelName is the name under which artifact references are stored on an element.
	LabelName = "ocm.software/artifact-references"
	// Version is the specification version of the label value implemented here.
	Version = "v1alpha1"
)

// Reference is one entry of the artifact references label value. Its Identity selects
// the artifact being described.
type Reference struct {
	Identity runtime.Identity `json:"identity"`
}

// References is the decoded value of the artifact references label. The spec models it
// as a list, so one element may describe several subjects.
type References []Reference

// Describes checks whether any of the references points to the subject artifact.
func (r References) Describes(subject runtime.Identity) bool {
	return slices.ContainsFunc(r, func(reference Reference) bool {
		return reference.Describes(subject)
	})
}

// Describes checks whether the reference points to the subject artifact. The following
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

// FromLabels decodes the artifact reference label out of labels.
func FromLabels(labels []descriptor.Label) (References, bool, error) {
	for _, label := range labels {
		if label.Name != LabelName {
			continue
		}
		if label.Version != "" && label.Version != Version {
			continue
		}
		var refs References
		if err := label.GetValue(&refs); err != nil {
			return nil, false, fmt.Errorf("interpreting label %q failed: %w", LabelName, err)
		}
		return refs, true, nil
	}
	return nil, false, nil
}

// FindDescribingResources returns every resource in desc whose artifact reference label
// selects the target, ordered by appearance.
func FindDescribingResources(desc *descriptor.Descriptor, target runtime.Identity) ([]*descriptor.Resource, error) {
	var matches []*descriptor.Resource
	for i := range desc.Component.Resources {
		resource := &desc.Component.Resources[i]
		refs, ok, err := FromLabels(resource.Labels)
		if err != nil {
			return nil, fmt.Errorf("resource %q: %w", resource.ToIdentity(), err)
		}
		if !ok || !refs.Describes(target) {
			continue
		}
		matches = append(matches, resource)
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, target)
	}
	return matches, nil
}
