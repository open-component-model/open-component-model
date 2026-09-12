// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package gpg

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/gpg/signing/handler"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
)

func Register(
	signingHandlerRegistry *signinghandler.SigningRegistry,
	credentialTypeRegistry *credentialtyperepository.CredentialTypeRegistry,
) error {
	hdlr, err := handler.New(nil)
	if err != nil {
		return err
	}

	if err := credentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(hdlr); err != nil {
		return fmt.Errorf("could not register GPG credential types: %w", err)
	}

	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(hdlr)
}
