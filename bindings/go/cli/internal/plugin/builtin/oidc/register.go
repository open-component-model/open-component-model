package oidc

import (
	"fmt"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialplugin"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/handler"
)

// Register registers the Sigstore signing handler with the signing registry and
// registers OIDCIdentityToken/v1alpha1 and TrustedRoot/v1alpha1 credential types.
func Register(
	signingHandlerRegistry *signinghandler.SigningRegistry,
	credentialTypeRegistry *credentialtyperepository.CredentialTypeRegistry,
	filesystemConfig *filesystemv1alpha1.Config,
) error {
	var tempDir string
	if filesystemConfig.TempFolder != nil {
		tempDir = *filesystemConfig.TempFolder
	}
	hdlr := handler.New(handler.WithTempDir(tempDir))

	if err := credentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(hdlr); err != nil {
		return fmt.Errorf("could not register Sigstore credential types: %w", err)
	}

	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(hdlr)
}

// RegisterCredentialPlugin registers the OIDC credential plugin with the credential plugin registry.
func RegisterCredentialPlugin(registry *credentialplugin.Registry) error {
	return registry.RegisterInternalCredentialPlugin(&OIDCPlugin{})
}
