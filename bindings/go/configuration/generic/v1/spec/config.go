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

// MergeConfigs merges multiple generic configs into one by appending them.
// It takes `warnFn` as an argument to allow for caller defined warning on
// any encountered nested generic configs, which are not supported.
func MergeConfigs(warnFn func(msg string, keysAndValues ...any), configs ...*Config) *Config {
	merged := new(Config)
	merged.Configurations = make([]*runtime.Raw, 0)
	for _, config := range configs {
		for _, entry := range config.Configurations {
			if Scheme.IsRegistered(entry.GetType()) {
				warnFn(
					"ignoring nested configuration: nested generic configurations are not supported, move the nested entries to the top-level configurations list",
					"type", entry.GetType().String(),
					"value", entry.Data,
				)
				continue
			}
			merged.Configurations = append(merged.Configurations, entry)
		}
	}
	return merged
}

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
