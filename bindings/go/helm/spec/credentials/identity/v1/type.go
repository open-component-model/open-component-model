package v1

import (
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
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

// AcceptedCredentialTypes declares which credential types are valid for HTTP Helm repositories.
func (h *HelmChartRepositoryIdentity) AcceptedCredentialTypes() []runtime.Type {
	return []runtime.Type{
		runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version),
	}
}

// ToIdentity converts the typed identity struct into a runtime.Identity map for graph lookup.
func (h *HelmChartRepositoryIdentity) ToIdentity() runtime.Identity {
	id := runtime.Identity{}
	id.SetType(VersionedType)
	if h.Hostname != "" {
		id[runtime.IdentityAttributeHostname] = h.Hostname
	}
	if h.Scheme != "" {
		id[runtime.IdentityAttributeScheme] = h.Scheme
	}
	if h.Port != "" {
		id[runtime.IdentityAttributePort] = h.Port
	}
	if h.Path != "" {
		id[runtime.IdentityAttributePath] = h.Path
	}
	return id
}
