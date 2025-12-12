package v1

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// DockerConfig is a type that represents a docker config style credential repository.
//
// Credentials can be offered
// - inline as encoded JSON (DockerConfig)
// - as a file reference (DockerConfigFile)
//
// As a special case, If neither DockerConfigFile nor DockerConfig are set, the following logic applies:
// If the $DOCKER_CONFIG environment variable is set, $DOCKER_CONFIG/config.json should be used.
// Otherwise, the default location $HOME/.docker/config.json should be used.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type DockerConfig struct {
	// +ocm:jsonschema-gen:enum=DockerConfig/v1
	// +ocm:jsonschema-gen:enum:deprecated=DockerConfig
	Type runtime.Type `json:"type"`
	// The reference path to the docker config JSON
	DockerConfigFile string `json:"dockerConfigFile,omitempty"`
	DockerConfig     string `json:"dockerConfig,omitempty"`
}

func (c *DockerConfig) String() string {
	base := c.GetType().String()
	if c.DockerConfigFile != "" {
		base += fmt.Sprintf("(%s)", c.DockerConfigFile)
	}
	if c.DockerConfig != "" {
		base += "(inline)"
	}
	return base
}
