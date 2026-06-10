// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package gpg

import (
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/gpg/signing/handler"
	gpgcredsv1alpha1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(
	signingHandlerRegistry *signinghandler.SigningRegistry,
	repositoryRegistry *credentialrepository.RepositoryRegistry,
	_ *filesystemv1alpha1.Config,
) error {
	// has no scheme in released bindings yet
	gpgCredScheme := runtime.NewScheme()
	gpgcredsv1alpha1.MustRegisterCredentialType(gpgCredScheme)
	repositoryRegistry.Register(gpgCredScheme)

	hdlr, err := handler.New(nil)
	if err != nil {
		return err
	}

	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(hdlr)
}
