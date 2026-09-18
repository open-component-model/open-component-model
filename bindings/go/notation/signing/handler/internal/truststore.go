package internal

import (
	"context"
	"crypto/x509"
	"fmt"

	"github.com/notaryproject/notation-go/verifier/truststore"
)

// NamedStore is the fixed named store used in the in-code blob trust policy
// (referenced as "ca:ocm" in the TrustStores field).
const NamedStore = "ocm"

// InMemoryTrustStore implements notation-go's [truststore.X509TrustStore] over
// an in-memory set of CA certificates supplied via OCM credentials, avoiding
// any dependency on notation's on-disk trust store layout.
type InMemoryTrustStore struct {
	certificates []*x509.Certificate
}

// NewInMemoryTrustStore returns a trust store serving certs for the CA store
// type under [NamedStore].
func NewInMemoryTrustStore(certs []*x509.Certificate) *InMemoryTrustStore {
	return &InMemoryTrustStore{certificates: certs}
}

// GetCertificates returns the configured CA certificates for storeType
// [truststore.TypeCA] and namedStore [NamedStore]; any other combination
// yields an error, matching notation's expectation that a policy references an
// existing store.
func (s *InMemoryTrustStore) GetCertificates(_ context.Context, storeType truststore.Type, namedStore string) ([]*x509.Certificate, error) {
	if storeType != truststore.TypeCA {
		return nil, fmt.Errorf("unsupported trust store type %q (only %q is supported)", storeType, truststore.TypeCA)
	}
	if namedStore != NamedStore {
		return nil, fmt.Errorf("unknown named trust store %q (only %q is supported)", namedStore, NamedStore)
	}
	if len(s.certificates) == 0 {
		return nil, fmt.Errorf("no trusted CA certificates configured")
	}
	return s.certificates, nil
}
