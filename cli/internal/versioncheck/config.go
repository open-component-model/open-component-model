package versioncheck

import (
	"fmt"

	generic "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	ConfigType    = "versioncheck.config.ocm.software"
	ConfigVersion = "v1alpha1"
)

var configScheme = runtime.NewScheme()

func init() {
	configScheme.MustRegisterWithAlias(&Config{}, runtime.NewVersionedType(ConfigType, ConfigVersion))
}

type Config struct {
	Type     runtime.Type `json:"type"`
	Disabled bool         `json:"disabled"`
}

func (c *Config) GetType() runtime.Type {
	return c.Type
}

func (c *Config) SetType(typ runtime.Type) {
	c.Type = typ
}

func (c *Config) DeepCopy() *Config {
	if c == nil {
		return nil
	}
	out := *c
	return &out
}

func (c *Config) DeepCopyTyped() runtime.Typed {
	if copy := c.DeepCopy(); copy != nil {
		return copy
	}
	return nil
}

func LookupConfig(cfg *generic.Config) (*Config, error) {
	if cfg == nil {
		return &Config{}, nil
	}

	filtered, err := generic.Filter(cfg, &generic.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(ConfigType, ConfigVersion),
			runtime.NewUnversionedType(ConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter versioncheck config: %w", err)
	}

	for _, entry := range filtered.Configurations {
		var config Config
		if err := configScheme.Convert(entry, &config); err != nil {
			return nil, fmt.Errorf("failed to decode versioncheck config: %w", err)
		}
		if config.Disabled {
			return &config, nil
		}
	}

	return &Config{}, nil
}
