package v1

import (
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const RelativeOCIReferenceType = "relativeOciReference"

// RelativeOCIReference describes a read/transfer compatibility access migrated from
// OCM v1. Reference is an OCI artifact reference (repository[:tag][@digest]) relative to
// the registry root that hosts the component version — not the OCM subPath. It carries no
// globally resolvable registry host: resolving it requires the component version's own
// repository.
//
// v2 never emits this type on write; it exists only to read and transfer migrated v1
// component versions. See website/content/docs/reference/input-and-access-types.md.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type RelativeOCIReference struct {
	// +ocm:jsonschema-gen:enum=relativeOciReference/v1
	// +ocm:jsonschema-gen:enum:deprecated=relativeOciReference
	Type runtime.Type `json:"type"`
	// Reference is the registry-relative OCI artifact reference
	// (repository[:tag][@digest]); no scheme, no leading slash, no host:port prefix.
	Reference string `json:"reference"`
}

// Validate verifies that the relative reference is set and well formed. The reference is a
// registry-relative OCI reference (repository[:tag][@digest]); a dotted first segment is a
// path, not a host.
func (t *RelativeOCIReference) Validate() error {
	if t.Reference == "" {
		return errors.New("reference is required")
	}
	if _, err := looseref.ParseReference(t.Reference); err != nil {
		return fmt.Errorf("invalid reference %q: %w", t.Reference, err)
	}
	return nil
}

func (t *RelativeOCIReference) String() string {
	return t.Reference
}
