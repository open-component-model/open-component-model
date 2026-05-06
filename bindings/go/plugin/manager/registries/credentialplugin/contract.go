package credentialplugin

import (
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// BuiltinCredentialPlugin allows plugin registries to register internal
// plugins without requiring callers to explicitly provide a scheme with
// their supported types.
type BuiltinCredentialPlugin interface {
	credentials.CredentialPlugin
	GetCredentialPluginScheme() *runtime.Scheme
}
