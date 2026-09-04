package oidc

import (
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialplugin"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/handler"
	"ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/oidcidentitytoken"
	"ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/trustedroot"
)

// Register registers the Sigstore signing handler with the signing registry and
// registers OIDCIdentityToken/v1alpha1 and TrustedRoot/v1alpha1 credential types.
func Register(
	signingHandlerRegistry *signinghandler.SigningRegistry,
	repositoryRegistry *credentialrepository.RepositoryRegistry,
	filesystemConfig *filesystemv1alpha1.Config,
) error {
	repositoryRegistry.Register(oidcidentitytoken.Scheme)
	repositoryRegistry.Register(trustedroot.Scheme)

	var tempDir string
	if filesystemConfig.TempFolder != nil {
		tempDir = *filesystemConfig.TempFolder
	}
	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(
		handler.New(handler.WithTempDir(tempDir)),
	)
}

// RegisterCredentialPlugin registers the OIDC credential plugin with the credential plugin registry.
func RegisterCredentialPlugin(registry *credentialplugin.Registry) error {
	return registry.RegisterInternalCredentialPlugin(&OIDCPlugin{})
}
