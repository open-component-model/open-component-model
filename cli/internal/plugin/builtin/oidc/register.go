package oidc

import (
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialplugin"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/handler"
)

// Register registers the Sigstore signing handler with the signing registry.
func Register(signingHandlerRegistry *signinghandler.SigningRegistry, filesystemConfig *filesystemv1alpha1.Config) error {
	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(
		handler.New(handler.WithTempDir(filesystemConfig.TempFolder)),
	)
}

// RegisterCredentialPlugin registers the OIDC credential plugin with the credential plugin registry.
func RegisterCredentialPlugin(registry *credentialplugin.Registry) error {
	return registry.RegisterInternalCredentialPlugin(&OIDCPlugin{})
}
