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
	credTypeRegistry *credentialtyperepository.CredentialTypeRegistry,
	filesystemConfig *filesystemv1alpha1.Config,
) error {
	hldr := handler.New(handler.WithTempDir(filesystemConfig.TempFolder))
	if err := credTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(hldr); err != nil {
		return fmt.Errorf("could not register OIDC credential type plugin: %w", err)
	}

	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(
		hldr,
	)
}

// RegisterCredentialPlugin registers the OIDC credential plugin with the credential plugin registry.
func RegisterCredentialPlugin(registry *credentialplugin.Registry) error {
	return registry.RegisterInternalCredentialPlugin(&OIDCPlugin{})
}
