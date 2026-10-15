package v1

import (
	"errors"
	"fmt"
	"strings"

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

// Validate verifies that the relative reference is set and is registry-relative: a bare
// repository[:tag][@digest], with no scheme, no leading slash, and no host. The host is
// supplied by the component repository at resolution time (our resolution prepends the
// hosting registry verbatim), so a reference must not carry one.
//
// This mirrors OCM v1, which resolves a relative reference with oci.ParseArt — a host-less
// artifact grammar that rejects exactly a scheme, a leading slash, and a host:port. v1 has
// no spec-level validator and enforces this at resolution; we check it up front for a clear
// error. looseref.ParseReference cannot by itself tell a registry-relative reference from an
// absolute one: it parses the first path segment as the "registry", so a legitimate
// multi-segment or dotted path ("ocm/value", "acme.org/value") lands in .Registry. A
// non-empty .Registry therefore does NOT indicate a host; only a colon in it (a host:port)
// is never a valid repository path.
func (t *RelativeOCIReference) Validate() error {
	if t.Reference == "" {
		return errors.New("reference is required")
	}
	if strings.HasPrefix(t.Reference, "/") {
		return fmt.Errorf("invalid reference %q: must be registry-relative (no leading slash)", t.Reference)
	}
	ref, err := looseref.ParseReference(t.Reference)
	if err != nil {
		return fmt.Errorf("invalid reference %q: %w", t.Reference, err)
	}
	if ref.Scheme != "" {
		return fmt.Errorf("invalid reference %q: must be registry-relative (no scheme)", t.Reference)
	}
	if strings.ContainsRune(ref.Registry, ':') {
		return fmt.Errorf("invalid reference %q: must be registry-relative (no host:port)", t.Reference)
	}
	return nil
}

func (t *RelativeOCIReference) String() string {
	return t.Reference
}
