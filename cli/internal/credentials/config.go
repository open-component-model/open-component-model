package credentials

import (
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	credentialsRuntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	credentialsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var scheme = runtime.NewScheme()

func init() {
	credentialsv1.MustRegister(scheme)
}

// LookupCredentialConfiguration creates a new ConfigCredentialProvider from a central V1 config.
func LookupCredentialConfiguration(cfg *genericv1.Config) (*credentialsRuntime.Config, error) {
	if cfg == nil || len(cfg.Configurations) == 0 {
		return &credentialsRuntime.Config{}, nil
	}
	cfg, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(credentialsv1.ConfigType, credentialsv1.Version),
			runtime.NewUnversionedType(credentialsv1.ConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter config: %w", err)
	}
	credentialConfigs := make([]*credentialsRuntime.Config, 0, len(cfg.Configurations))
	for _, entry := range cfg.Configurations {
		var credentialConfig credentialsv1.Config
		if err := scheme.Convert(entry, &credentialConfig); err != nil {
			return nil, fmt.Errorf("failed to decode credential config: %w", err)
		}
		converted := credentialsRuntime.ConvertFromV1(&credentialConfig)
		credentialConfigs = append(credentialConfigs, converted)
	}
	return credentialsRuntime.Merge(credentialConfigs...), nil
}
