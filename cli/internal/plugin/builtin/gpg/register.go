// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package gpg

import (
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/gpg/signing/handler"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
)

func Register(
	signingHandlerRegistry *signinghandler.SigningRegistry,
	_ *filesystemv1alpha1.Config,
) error {
	hdlr, err := handler.New(nil)
	if err != nil {
		return err
	}

	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(hdlr)
}
