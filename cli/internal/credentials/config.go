package credentials

import (
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	credentialsRuntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
)

// LookupCredentialConfiguration creates a new ConfigCredentialProvider from a central V1 config.
func LookupCredentialConfiguration(cfg *genericv1.Config) (*credentialsRuntime.Config, error) {
	return credentialsRuntime.ExtractCredentialConfig(cfg)
}
