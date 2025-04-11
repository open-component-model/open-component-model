package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var scheme = runtime.NewScheme()

func init() {
	scheme.MustRegisterWithAlias(&Config{}, runtime.NewVersionedType(ConfigType, ConfigTypeV1))
}

const (
	ConfigType   = "generic.config.ocm.software"
	ConfigTypeV1 = "v1"
)

// Config holds configuration entities loaded through a configuration file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
	Type           runtime.Type   `json:"type"`
	Configurations []*runtime.Raw `json:"configurations"`
}
