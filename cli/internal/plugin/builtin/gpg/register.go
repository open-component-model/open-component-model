package gpg

import (
	"errors"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/gpg/signing/handler"
	"ocm.software/open-component-model/bindings/go/gpg/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(
	signingHandlerRegistry *signinghandler.SigningRegistry,
	_ *filesystemv1alpha1.Config,
) error {
	scheme := runtime.NewScheme()
	if err := scheme.RegisterScheme(v1alpha1.Scheme); err != nil {
		return err
	}

	hdlr, err := handler.New(scheme)
	if err != nil {
		return err
	}

	return errors.Join(
		signingHandlerRegistry.RegisterInternalComponentSignatureHandler(hdlr),
	)
}
