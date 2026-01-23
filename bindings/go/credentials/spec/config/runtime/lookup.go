package runtime

import (
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	credentialsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var scheme = runtime.NewScheme()

func init() {
	credentialsv1.MustRegister(scheme)
}

// LookupCredentialConfig extracts credential configuration from a generic configuration.
// It filters the configuration for credential entries and merges them into a single config.
func LookupCredentialConfig(config *genericv1.Config) (*Config, error) {
	if config == nil || len(config.Configurations) == 0 {
		return &Config{}, nil
	}

	filtered, err := genericv1.Filter(config, &genericv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(credentialsv1.ConfigType, credentialsv1.Version),
			runtime.NewUnversionedType(credentialsv1.ConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter config: %w", err)
	}

	credentialConfigs := make([]*Config, 0, len(filtered.Configurations))
	for _, entry := range filtered.Configurations {
		var credentialConfig credentialsv1.Config
		if err := scheme.Convert(entry, &credentialConfig); err != nil {
			return nil, fmt.Errorf("failed to decode credential config: %w", err)
		}
		converted := ConvertFromV1(&credentialConfig)
		credentialConfigs = append(credentialConfigs, converted)
	}

	return Merge(credentialConfigs...), nil
}
