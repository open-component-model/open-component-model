package v1

import (
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OCIRegistryIdentity is the typed consumer identity for OCI container registries.
// It describes the target registry by hostname, scheme, port, and path.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCIRegistryIdentity struct {
	// +ocm:jsonschema-gen:enum=OCIRegistry/v1
	// +ocm:jsonschema-gen:enum:deprecated=OCIRegistry
	Type     runtime.Type `json:"type"`
	Hostname string       `json:"hostname,omitempty"`
	Scheme   string       `json:"scheme,omitempty"`
	Port     string       `json:"port,omitempty"`
	Path     string       `json:"path,omitempty"`
}

// AcceptedCredentialTypes declares which credential types are valid for OCI registries.
func (o *OCIRegistryIdentity) AcceptedCredentialTypes() []runtime.Type {
	return []runtime.Type{
		runtime.NewVersionedType(ocicredsv1.OCICredentialsType, ocicredsv1.Version),
	}
}

// ToIdentity converts the typed identity struct into a runtime.Identity map for graph lookup.
func (o *OCIRegistryIdentity) ToIdentity() runtime.Identity {
	id := runtime.Identity{}
	id.SetType(VersionedType)
	if o.Hostname != "" {
		id[runtime.IdentityAttributeHostname] = o.Hostname
	}
	if o.Scheme != "" {
		id[runtime.IdentityAttributeScheme] = o.Scheme
	}
	if o.Port != "" {
		id[runtime.IdentityAttributePort] = o.Port
	}
	if o.Path != "" {
		id[runtime.IdentityAttributePath] = o.Path
	}
	return id
}
