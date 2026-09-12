package credentialtyperepository

import (
	"ocm.software/open-component-model/bindings/go/credentials"
)

type BuiltinCredentialTypeSchemeProviderPlugin interface {
	credentials.CredentialTypeSchemeProvider
}
