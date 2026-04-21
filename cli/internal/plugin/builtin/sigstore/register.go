package sigstore

import (
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/handler"
)

// Register registers the Sigstore signing handler with the signing registry.
func Register(signingHandlerRegistry *signinghandler.SigningRegistry) error {
	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(handler.New())
}
