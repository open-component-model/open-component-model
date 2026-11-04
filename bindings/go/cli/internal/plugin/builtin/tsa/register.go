package tsa

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/signing/tsa"
)

// Register registers the TSA credential and consumer identity types. TSA root
// certificates are resolved during verification independent of the signing handler.
func Register(credentialTypeRegistry *credentialtyperepository.CredentialTypeRegistry) error {
	if err := credentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(tsa.CredentialTypes{}); err != nil {
		return fmt.Errorf("could not register TSA credential types: %w", err)
	}
	return nil
}
