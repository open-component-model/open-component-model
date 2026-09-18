package v1

import (
	"strings"

	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	GitIdentityType = "Git"
	Version         = "v1"
)

var (
	Type          = runtime.NewUnversionedType(GitIdentityType)
	VersionedType = runtime.NewVersionedType(GitIdentityType, Version)
)

// GitIdentity scopes credentials to a Git endpoint and repository path.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GitIdentity struct {
	// +ocm:jsonschema-gen:enum=Git/v1
	// +ocm:jsonschema-gen:enum:deprecated=Git
	Type     runtime.Type `json:"type"`
	Hostname string       `json:"hostname,omitempty"`
	Scheme   string       `json:"scheme,omitempty"`
	Port     string       `json:"port,omitempty"`
	Path     string       `json:"path,omitempty"`
}

func IdentityFromURL(repository string) (runtime.Identity, error) {
	ep, err := endpoint.Parse(repository)
	if err != nil {
		return nil, err
	}

	hostname := endpoint.Hostname(ep)
	if ep.Protocol == "file" {
		hostname = "localhost"
	}

	id := runtime.Identity{
		runtime.IdentityAttributeHostname: hostname,
		runtime.IdentityAttributeScheme:   ep.Protocol,
		runtime.IdentityAttributePath:     strings.TrimPrefix(ep.Path, "/"),
	}

	if port := endpoint.Port(ep); port != "" {
		id[runtime.IdentityAttributePort] = port
	}
	id.SetType(Type)

	return id, nil
}
