package credentials

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CredentialAcceptor is an optional interface that typed identity structs can implement
// to declare which credential types they accept. The graph validates during ingestion
// that configured credential types are compatible with the identity type.
type CredentialAcceptor interface {
	AcceptedCredentialTypes() []runtime.Type
}

// TypeSchemeProvider provides read access to a runtime.Scheme of known types.
// The credential graph uses this during ingestion to deserialize typed credentials,
// resolve type aliases, and validate identity ↔ credential compatibility.
type TypeSchemeProvider interface {
	Scheme() *runtime.Scheme
}
