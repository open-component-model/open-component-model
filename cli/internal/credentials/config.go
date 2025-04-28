package credentials

import (
	"fmt"

	credentialsRuntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	credentialsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/cli/configuration/v1"
)

var scheme = runtime.NewScheme()

func init() {
	credentialsv1.MustRegister(scheme)
}

// LookupCredentialConfiguration creates a new ConfigCredentialProvider from a central V1 config.
func LookupCredentialConfiguration(cfg *v1.Config) (*credentialsRuntime.Config, error) {
	cfg, err := v1.Filter(cfg, &v1.FilterOptions{
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
