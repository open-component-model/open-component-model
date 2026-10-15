package notation

import (
	"fmt"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/notation/signing/handler"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
)

// Register registers the Notation signing handler with the signing registry and
// its credential types with the credential type registry.
func Register(
	signingHandlerRegistry *signinghandler.SigningRegistry,
	credentialTypeRegistry *credentialtyperepository.CredentialTypeRegistry,
	_ *filesystemv1alpha1.Config,
) error {
	hdlr := handler.New()
	if err := credentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(hdlr); err != nil {
		return fmt.Errorf("could not register Notation credential types: %w", err)
	}
	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(hdlr)
}
