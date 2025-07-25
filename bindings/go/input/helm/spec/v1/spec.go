package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Helm describes an input sourced by a file system directory having a typical helm chart structure:
// Chart.yaml, values.yaml, charts/, templates/, ...

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Helm struct {
	Type runtime.Type `json:"type"`

	// Path is the path to the directory.
	Path string `json:"path"`

	// Repository field property can be used to specify the repository hint for the generated local artifact access.
	// It is prefixed by the component name, if	it does not start with slash "/".
	// The repository hint is a full OCI repository reference, where the helm chart needs to be uploaded to.
	// TODO(ikhandamirov,jakobmoellerdev): decide what to do, if the field is set.
	Repository string `json:"repository,omitempty"`

	// Version field is included for compatibility with previous OCM version.
	// Support for this option is not implemented yet. The Version of the resource is taken from the Chart.yaml file.
	Version string `json:"version,omitempty"`

	// HelmRepository was used in previous OCM versions specify, to if the helm chart should be loaded from
	// a helm repository instead of the local filesystem. Support for this option is not implemented yet.
	HelmRepository string `json:"helmRepository,omitempty"`

	// CACert was used in previous OCM versions in combination with HelmRepository to specify a TLS root certificate
	// used to access the source helm repository. Support for this option is not implemented yet.
	CACert string `json:"caCert,omitempty"`

	// CACertFile was used in previous OCM versions in combination with HelmRepository to specify a relative filename
	// for TLS root certificate, used to access the source helm repository.
	// Support for this option is not implemented yet.
	CACertFile string `json:"caCertFile,omitempty"`
}

func (t *Helm) String() string {
	return t.Path
}

const (
	Version = "v1"
	Type    = "helm"
)
