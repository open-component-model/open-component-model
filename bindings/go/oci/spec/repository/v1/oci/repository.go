package oci

import (
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	LegacyRegistryType  = "OCIRegistry"
	LegacyRegistryType2 = "ociRegistry"
	ShortType           = "OCI"
	ShortType2          = "oci"
	Type                = "OCIRepository"
)

// Repository is a type that represents an OCI repository as per
// https://github.com/opencontainers/distribution-spec
//
// It is not only used to specify the full OCI compliant repository namespace, but also contains
// a full URL in which the scheme can indicate support for https or http. the oci scheme is also recognized.
// Additionally, the port can be used to specify a port for the repository.
// Note that any path here is used to specify the root path of the OCI Repository.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Repository struct {
	// +ocm:jsonschema-gen:enum=OCIRepository/v1,OCI/v1,oci/v1,OCIRegistry/v1,ociRegistry/v1
	// +ocm:jsonschema-gen:enum:deprecated=OCIRepository,OCI,oci,OCIRegistry,ociRegistry
	Type runtime.Type `json:"type"`
	// BaseURL is the base url of the OCI registry (host + optional port).
	// Should not include repository paths - use SubPath for that.
	//
	// Examples:
	//   - "https://registry.example.com"
	//   - "https://registry.example.com:5000"
	//   - "oci://registry.example.com:5000"
	//   - "docker.io"
	//   - "ghcr.io"
	//
	// If BaseUrl contains a path (e.g., "ghcr.io/org/repo"),
	// the path will be auto-extracted and used as SubPath.
	BaseUrl string `json:"baseUrl"`
	// SubPath is an optional repository prefix path used for the OCM repository.
	// The OCM-based artifacts will use this path as a repository prefix.
	// An OCI registry may host many OCM repositories with different repository prefixes.
	//
	// Auto-extraction: If not specified and BaseUrl contains a path component,
	// the path will be automatically extracted and used as SubPath.
	//
	// Examples:
	//   Explicit separation:
	//     BaseUrl="ghcr.io" + SubPath="open-component-model/ocm"
	//     → Registry: ghcr.io, Repository prefix: open-component-model/ocm
	//
	//   Embedded path:
	//     BaseUrl="ghcr.io/open-component-model/ocm" + SubPath=""
	//     → Auto-extracts to: BaseUrl="ghcr.io", SubPath="open-component-model/ocm"
	SubPath string `json:"subPath,omitempty"`
	// ComponentNameMapping is accepted for compatibility with OCM v1 specifications.
	// Only the default mapping (empty or "urlPath") is supported. The deprecated
	// "sha256-digest" mapping is rejected, see [ComponentNameMappingSHA256Digest].
	ComponentNameMapping string `json:"componentNameMapping,omitempty"`
}

const (
	// ComponentNameMappingURLPath is the default mapping: component names are used as repository path.
	ComponentNameMappingURLPath = "urlPath"
	// ComponentNameMappingSHA256Digest is the deprecated OCM v1 mapping that stores components
	// under the sha256 digest of their name. It is not supported.
	ComponentNameMappingSHA256Digest = "sha256-digest"
)

// ValidateComponentNameMapping returns an error if the spec requests an unsupported component name mapping.
// The error wraps [errors.ErrUnsupported], so that callers trying several repositories (such as fallback
// resolvers) can skip this one instead of aborting.
func (spec *Repository) ValidateComponentNameMapping() error {
	switch spec.ComponentNameMapping {
	case "", ComponentNameMappingURLPath:
		return nil
	case ComponentNameMappingSHA256Digest:
		return fmt.Errorf("componentNameMapping %q: %w: it is a deprecated OCM v1 mapping that "+
			"stores components under the sha256 digest of their name; re-transfer the component versions "+
			"with the default %q mapping", spec.ComponentNameMapping, errors.ErrUnsupported, ComponentNameMappingURLPath)
	default:
		return fmt.Errorf("componentNameMapping %q: %w: only %q is supported", spec.ComponentNameMapping, errors.ErrUnsupported, ComponentNameMappingURLPath)
	}
}

func (spec *Repository) String() string {
	return spec.BaseUrl
}

// GlobalAccessPolicy defines how global access references are handled when adding local blobs.
// The zero value (empty string) maps to never adding global access, discouraging reliance on
// global access references.
type GlobalAccessPolicy string

const (
	// GlobalAccessPolicyNever suppresses global access on all local blobs, even when the storage
	// backend is globally reachable. This is the default (zero value) to discourage reliance on
	// global access references.
	GlobalAccessPolicyNever GlobalAccessPolicy = ""
	// GlobalAccessPolicyAuto auto-detects based on the storage backend. Global access is only
	// added when the storage backend is globally reachable (e.g. a remote OCI registry).
	//
	// Experimental: This policy is carried over from OCM v1 for backwards compatibility.
	// Its future availability is being evaluated by the community.
	GlobalAccessPolicyAuto GlobalAccessPolicy = "auto"
)
