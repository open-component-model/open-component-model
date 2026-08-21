// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package gpg

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/gpg/signing/handler"
	gpgcredsv1alpha1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(
	signingHandlerRegistry *signinghandler.SigningRegistry,
	credTypeRegistry *credentialtyperepository.CredentialTypeRegistry,
) error {
	// has no scheme in released bindings yet
	gpgCredScheme := runtime.NewScheme()
	gpgcredsv1alpha1.MustRegisterCredentialType(gpgCredScheme)

	hdlr, err := handler.New(nil)
	if err != nil {
		return err
	}

	if err := credTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(hdlr); err != nil {
		return fmt.Errorf("could not register GPG credential type plugin: %w", err)
	}

	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(hdlr)
}
