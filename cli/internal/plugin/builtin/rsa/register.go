package rsa

import (
	"errors"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/rsa/signing/handler"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(
	signingHandlerRegistry *signinghandler.SigningRegistry,
	// TODO add filesystem and logging awareness to rsa handler
	_ *filesystemv1alpha1.Config,
) error {
	scheme := runtime.NewScheme()
	if err := scheme.RegisterScheme(v1alpha1.Scheme); err != nil {
		return err
	}

	h, err := handler.New(true)
	if err != nil {
		return err
	}

	return errors.Join(
		signinghandler.RegisterInternalComponentSignatureHandler(
			scheme,
			signingHandlerRegistry,
			h,
			&v1alpha1.Config{},
		),
	)
}
