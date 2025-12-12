package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	ShortType = "Dummy"
	Type      = "DummyRepository"
)

// Repository type is used by the tests in this package for a Type.
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
