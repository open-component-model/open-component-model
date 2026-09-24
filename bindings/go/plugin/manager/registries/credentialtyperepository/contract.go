package credentialtyperepository

import (
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type BuiltinCredentialTypeSchemeProviderPlugin interface {
	credentials.CredentialTypeSchemeProvider
}

// ConsumerIdentityTypeSchemeProvider is an optional extension of
// BuiltinCredentialTypeSchemeProviderPlugin for built-in plugins that resolve credentials
// for their own consumer identity types (e.g. the wget input method and resource
// repository). The scheme must register every type alias under which consumers address
// the plugin (e.g. HTTP for Wget), so that the credential graph can canonicalize
// config-authored consumer identities to the spelling identity producers use.
// Registration via RegisterInternalCredentialTypeSchemeProvider picks it up automatically.
type ConsumerIdentityTypeSchemeProvider interface {
	GetConsumerIdentityTypeScheme() *runtime.Scheme
}
