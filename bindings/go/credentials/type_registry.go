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

// CredentialTypeSchemeProvider provides read access to a scheme of known
// credential types. The credential graph uses this during ingestion to
// deserialize typed credentials and resolve type aliases.
type CredentialTypeSchemeProvider interface {
	Scheme() *runtime.Scheme
}

// IdentityTypeSchemeProvider provides read access to a scheme of known consumer
// identity types. The credential graph uses this during ingestion to validate
// that configured credential types are compatible with identity types.
type IdentityTypeSchemeProvider interface {
	Scheme() *runtime.Scheme
}

// SchemeAsCredentialTypeSchemeProvider wraps a raw *runtime.Scheme to satisfy
// the CredentialTypeSchemeProvider interface. Useful for tests.
type SchemeAsCredentialTypeSchemeProvider struct {
	S *runtime.Scheme
}

func (r *SchemeAsCredentialTypeSchemeProvider) Scheme() *runtime.Scheme { return r.S }

// SchemeAsIdentityTypeSchemeProvider wraps a raw *runtime.Scheme to satisfy
// the IdentityTypeSchemeProvider interface. Useful for tests.
type SchemeAsIdentityTypeSchemeProvider struct {
	S *runtime.Scheme
}

func (r *SchemeAsIdentityTypeSchemeProvider) Scheme() *runtime.Scheme { return r.S }
