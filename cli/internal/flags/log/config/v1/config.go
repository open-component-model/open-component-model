package v1

import (
	"fmt"

	v1 "ocm.software/open-component-model/bindings/go/configuration/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	ConfigType = "logging.config.ocm.software"
	Version    = "v1"
)

const (
	DefaultLookupPriority = 10
)

var scheme = runtime.NewScheme()

func init() {
	scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config is the OCM configuration type.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
	Type runtime.Type `json:"type"`

	// Settings defines generic logging settings.
	Settings Settings `json:"settings"`
}

// +k8s:deepcopy-gen=true
type Settings struct {
	// DefaultLevel defines the default logging level to use if no specific rule matches.
	DefaultLevel string `json:"defaultLevel,omitempty"`
	// Rules defines a list of rules that can be used to filter logs based on conditions.
	Rules []Rule `json:"rules,omitempty"`
}

// +k8s:deepcopy-gen=true
type Rule struct {
	// Level defines the logging level for this rule.
	Level string `json:"level"`
	// Conditions defines the conditions that must be met for this rule to apply.
	Conditions []Condition `json:"conditions"`
}

// +k8s:deepcopy-gen=true
type Condition struct {
	// Realm defines the realm for this condition.
	// Realm is a special attribute that is used across OCM to identify the categories of functionality.
	// It can be used to filter groups of logs.
	Realm string `json:"realm,omitempty"`
}

// Lookup creates a new Config from a central V1 config.
func Lookup(cfg *v1.Config) (*Config, error) {
	if cfg == nil {
		return nil, nil
	}
	cfg, err := v1.Filter(cfg, &v1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(ConfigType, Version),
			runtime.NewUnversionedType(ConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter config: %w", err)
	}
	cfgs := make([]*Config, 0, len(cfg.Configurations))
	for _, entry := range cfg.Configurations {
		var config Config
		if err := scheme.Convert(entry, &config); err != nil {
			return nil, fmt.Errorf("failed to decode credential config: %w", err)
		}
		cfgs = append(cfgs, &config)
	}
	return Merge(cfgs...), nil
}

// Merge merges the provided configs into a single config.
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	merged.Type = configs[0].Type
	merged.Settings = configs[0].Settings
	for _, config := range configs {
		if config.Type != merged.Type {
			continue // Skip configs with different types
		}

		if config.Settings.DefaultLevel != "" {
			// Only update DefaultLevel if it's not already set,
			// but allow overriding it if it is set in the current config
			merged.Settings.DefaultLevel = config.Settings.DefaultLevel
		}

		for _, rule := range config.Settings.Rules {
			// Check if the rule already exists
			exists := false
			for _, existingRule := range merged.Settings.Rules {
				if existingRule.Level == rule.Level && len(existingRule.Conditions) == len(rule.Conditions) {
					exists = true
					break
				}
			}
			if !exists {
				merged.Settings.Rules = append(merged.Settings.Rules, rule)
			}
		}
	}

	return merged
}
