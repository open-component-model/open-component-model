// Package config defines the CLI-internal central configuration type for the
// `ocm init` repository-classification command. It points the command at a Jev
// (TypeSafe System One) decisions endpoint + model and names the credential
// hostname used to resolve the API key from the OCM credential graph.
//
// It intentionally mirrors the shape of the versioncheck config
// (bindings/go/cli/internal/versioncheck/config.go): a package-private scheme,
// hand-written runtime.Typed methods, and a LookupConfig with a default fallback.
// It does NOT use the shared exported configuration Scheme or codegen — this is a
// CLI-internal type living under cli/internal.
package config

import (
	"fmt"

	generic "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ConfigType is the OCM configuration type identifier for the init classifier.
	ConfigType = "init.cli.config.ocm.software"
	// ConfigVersion is the schema version for the init classifier configuration.
	ConfigVersion = "v1alpha1"

	// DefaultEndpoint is the TypeSafe direct System One decisions endpoint.
	DefaultEndpoint = "https://api.typesafe.ai/v1/systemone"
	// DefaultModel is the default Jev model identifier.
	DefaultModel = "jev-latest"
	// DefaultCredentialHostname is the identity hostname used to resolve the API key.
	DefaultCredentialHostname = "api.typesafe.ai"
)

var configScheme = runtime.NewScheme()

func init() {
	configScheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, ConfigVersion),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config configures the `ocm init` classifier: which Jev decisions endpoint and
// model to call, and which credential hostname to resolve the API key under.
//
// Example config:
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	- type: init.cli.config.ocm.software/v1alpha1
//	  endpoint: https://openrouter.ai/api/alpha/decisions
//	  model: typesafe/jev-1.13
//	  credentialHostname: openrouter.ai
type Config struct {
	Type               runtime.Type `json:"type"`
	Endpoint           string       `json:"endpoint,omitempty"`
	Model              string       `json:"model,omitempty"`
	CredentialHostname string       `json:"credentialHostname,omitempty"`
}

func (c *Config) GetType() runtime.Type  { return c.Type }
func (c *Config) SetType(t runtime.Type) { c.Type = t }
func (c *Config) DeepCopyTyped() runtime.Typed {
	if c == nil {
		return nil
	}
	out := *c
	return &out
}

// LookupConfig extracts the init classifier configuration from a generic OCM
// config. It always returns a non-nil Config populated with the defaults; any
// non-empty field found in a matching configuration entry overrides its default.
// Later entries win (last-wins), matching the generic config precedence.
func LookupConfig(cfg *generic.Config) (*Config, error) {
	result := &Config{
		Type:               runtime.NewVersionedType(ConfigType, ConfigVersion),
		Endpoint:           DefaultEndpoint,
		Model:              DefaultModel,
		CredentialHostname: DefaultCredentialHostname,
	}
	if cfg == nil {
		return result, nil
	}

	filtered, err := generic.Filter(cfg, &generic.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(ConfigType, ConfigVersion),
			runtime.NewUnversionedType(ConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter init config: %w", err)
	}

	// Iterate forwards so later entries override earlier ones (last wins).
	for _, entry := range filtered.Configurations {
		var c Config
		if err := configScheme.Convert(entry, &c); err != nil {
			return nil, fmt.Errorf("failed to decode init config: %w", err)
		}
		if c.Endpoint != "" {
			result.Endpoint = c.Endpoint
		}
		if c.Model != "" {
			result.Model = c.Model
		}
		if c.CredentialHostname != "" {
			result.CredentialHostname = c.CredentialHostname
		}
	}

	return result, nil
}
