package oci

import (
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
}

func (spec *Repository) String() string {
	return spec.BaseUrl
}
