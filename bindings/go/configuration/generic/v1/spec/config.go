package spec

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{}, runtime.NewVersionedType(ConfigType, ConfigTypeV1))
	Scheme.MustRegisterWithAlias(&Config{}, runtime.NewUnversionedType(ConfigType))
}

const (
	ConfigType   = "generic.config.ocm.software"
	ConfigTypeV1 = Version
)

// Config holds configuration entities loaded through a configuration file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=generic.config.ocm.software/v1
	// +ocm:jsonschema-gen:enum:deprecated=generic.config.ocm.software
	Type           runtime.Type   `json:"type"`
	Configurations []*runtime.Raw `json:"configurations"`
}
