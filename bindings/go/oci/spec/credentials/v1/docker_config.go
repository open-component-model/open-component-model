package v1

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// DockerConfig is a type that represents a CTF repository interpreted as per
// https://github.com/opencontainers/distribution-spec and backed by a CTF.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type DockerConfig struct {
	Type             runtime.Type `json:"type"`
	DockerConfigFile string       `json:"dockerConfigFile,omitempty"`
	DockerConfig     string       `json:"dockerConfig,omitempty"`
}

func (c DockerConfig) String() string {
	base := fmt.Sprintf("%s", c.GetType())
	if c.DockerConfigFile != "" {
		base += fmt.Sprintf("(%s)", c.DockerConfigFile)
	}
	if c.DockerConfig != "" {
		base += fmt.Sprintf("(inline)")
	}
	return base
}

type GetCredentialRequest struct {
	Identity map[string]string `json:"identity"`
}
