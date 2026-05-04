package oidc

import (
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialplugin"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/handler"
)

// Register registers the Sigstore signing handler with the signing registry.
func Register(signingHandlerRegistry *signinghandler.SigningRegistry) error {
	h, err := handler.New()
	if err != nil {
		return err
	}
	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(h)
}

// RegisterCredentialPlugin registers the OIDC credential plugin with the credential plugin registry.
func RegisterCredentialPlugin(registry *credentialplugin.Registry) error {
	return registry.RegisterInternalCredentialPlugin(&OIDCPlugin{})
}
