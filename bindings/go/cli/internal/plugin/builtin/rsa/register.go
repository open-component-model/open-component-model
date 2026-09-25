package rsa

import (
	"errors"
	"fmt"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/rsa/signing/handler"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	tsacredentialsv1alpha1 "ocm.software/open-component-model/bindings/go/signing/tsa/spec/credentials/v1alpha1"
)

func Register(
	signingHandlerRegistry *signinghandler.SigningRegistry,
	credentialTypeRegistry *credentialtyperepository.CredentialTypeRegistry,
	// TODO add filesystem and logging awareness to rsa handler
	_ *filesystemv1alpha1.Config,
) error {
	scheme := runtime.NewScheme()
	if err := scheme.RegisterScheme(v1alpha1.Scheme); err != nil {
		return err
	}

	hdlr, err := handler.New(scheme, true)
	if err != nil {
		return err
	}

	if err := credentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(hdlr); err != nil {
		return fmt.Errorf("could not register RSA credential types: %w", err)
	}

	// TSA/v1alpha1 consumer credentials (RFC 3161 timestamp verification roots)
	// are resolved by the verify path; register the type so the credential graph
	// deserializes them into TSACredentials instead of falling back to DirectCredentials.
	tsaCredScheme := runtime.NewScheme()
	tsacredentialsv1alpha1.MustRegisterCredentialType(tsaCredScheme)
	if err := credentialTypeRegistry.Register(tsaCredScheme); err != nil {
		return fmt.Errorf("could not register TSA credential types: %w", err)
	}

	return errors.Join(
		signingHandlerRegistry.RegisterInternalComponentSignatureHandler(hdlr),
	)
}
