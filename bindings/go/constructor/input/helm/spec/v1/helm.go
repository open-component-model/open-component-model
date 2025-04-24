package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Helm is the spec for the helm input method.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Helm struct {
	Type runtime.Type `json:"type"`
	// PathSpec hold the path that points to the helm chart file
	Path           string `json:"path,omitempty"`
	HelmRepository string `json:"helmRepository,omitempty"`
	Version        string `json:"version,omitempty"`
	Repository     string `json:"repository,omitempty"`
	CACert         string `json:"caCert,omitempty"`
	CACertFile     string `json:"caCertFile,omitempty"`
}
