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
	Type runtime.Type `json:"type"`
	// BaseURL is the base url of the repository to resolve artifacts.
	//
	// Examples
	//   - https://registry.example.com
	//   - https://registry.example.com:5000
	//   - oci://registry.example.com:5000
	//   - docker.io
	//   - ghcr.io/open-component-model/ocm
	BaseUrl string `json:"baseUrl"`
}

func (spec *Repository) String() string {
	return spec.BaseUrl
}
