package notation

import (
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/notation/signing/handler"
	notationcreds "ocm.software/open-component-model/bindings/go/notation/spec/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
)

// Register registers the Notation signing handler with the signing registry and
// registers the NotationCredentials/v1 credential type.
func Register(
	signingHandlerRegistry *signinghandler.SigningRegistry,
	repositoryRegistry *credentialrepository.RepositoryRegistry,
	_ *filesystemv1alpha1.Config,
) error {
	repositoryRegistry.Register(notationcreds.Scheme)
	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(handler.New())
}
