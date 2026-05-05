package oidc

import (
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialplugin"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/handler"
)

// Register registers the Sigstore signing handler with the signing registry.
func Register(signingHandlerRegistry *signinghandler.SigningRegistry) error {
	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(handler.New())
}

// RegisterCredentialPlugin registers the OIDC credential plugin with the credential plugin registry.
func RegisterCredentialPlugin(registry *credentialplugin.Registry) error {
	return registry.RegisterInternalCredentialPlugin(&OIDCPlugin{})
}
