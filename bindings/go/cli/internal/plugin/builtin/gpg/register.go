// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package gpg

import (
	"fmt"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/gpg/signing/handler"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
)

// Register registers the GPG signing handler and its credential types. Temporary files and
// GnuPG home directories go to the tempFolder of the filesystem configuration.
func Register(
	signingHandlerRegistry *signinghandler.SigningRegistry,
	credentialTypeRegistry *credentialtyperepository.CredentialTypeRegistry,
	filesystemConfig *filesystemv1alpha1.Config,
) error {
	var tempDir string
	if filesystemConfig != nil && filesystemConfig.TempFolder != nil {
		tempDir = *filesystemConfig.TempFolder
	}
	hdlr, err := handler.New(nil, handler.WithTempDir(tempDir))
	if err != nil {
		return err
	}

	if err := credentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(hdlr); err != nil {
		return fmt.Errorf("could not register GPG credential types: %w", err)
	}

	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(hdlr)
}
