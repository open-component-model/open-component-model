package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	HelmChartRepositoryIdentityType = "HelmChartRepository"
	Version                         = "v1"
)

// Type is the unversioned consumer identity type for Helm chart repositories (backward compat).
var Type = runtime.NewUnversionedType(HelmChartRepositoryIdentityType)

// VersionedType is the versioned consumer identity type.
var VersionedType = runtime.NewVersionedType(HelmChartRepositoryIdentityType, Version)

// HelmChartRepositoryIdentity is the typed consumer identity for HTTP-based Helm chart repositories.
// OCI-based Helm repos use the OCIRegistry identity type instead.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type HelmChartRepositoryIdentity struct {
	// +ocm:jsonschema-gen:enum=HelmChartRepository/v1
	// +ocm:jsonschema-gen:enum:deprecated=HelmChartRepository
	Type     runtime.Type `json:"type"`
	Hostname string       `json:"hostname,omitempty"`
	Scheme   string       `json:"scheme,omitempty"`
	Port     string       `json:"port,omitempty"`
	Path     string       `json:"path,omitempty"`
}
