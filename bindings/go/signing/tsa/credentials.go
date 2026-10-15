package tsa

import (
	"crypto/x509"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/runtime"
	tsacredentials "ocm.software/open-component-model/bindings/go/signing/tsa/spec/credentials"
	tsacredentialsv1alpha1 "ocm.software/open-component-model/bindings/go/signing/tsa/spec/credentials/v1alpha1"
	tsaidentity "ocm.software/open-component-model/bindings/go/signing/tsa/spec/identity"
	tsaidentityv1alpha1 "ocm.software/open-component-model/bindings/go/signing/tsa/spec/identity/v1alpha1"
)

// CredentialTypes provides the TSA credential and consumer identity types for
// registration with a credential type registry.
type CredentialTypes struct{}

// GetCredentialTypeScheme returns the scheme with the TSA credential types.
func (CredentialTypes) GetCredentialTypeScheme() *runtime.Scheme {
	return tsacredentials.Scheme
}

// GetConsumerIdentityTypeScheme returns the scheme with the TSA consumer identity types.
func (CredentialTypes) GetConsumerIdentityTypeScheme() *runtime.Scheme {
	return tsaidentity.Scheme
}

const (
	// TSAURLLabelPrefix is the label name prefix used to store the TSA URL
	// in a component descriptor. The full label name is the prefix followed by
	// the signature name (e.g. "url.tsa.ocm.software/default").
	TSAURLLabelPrefix = "url.tsa.ocm.software/"
)

// TSAConsumerIdentity builds the credential consumer identity for a TSA server.
// A URL is decomposed into scheme/hostname/port/path so the credential graph can
// match URL-specific entries; the unversioned type is used because the graph
// canonicalizes configured TSA identities to it.
func TSAConsumerIdentity(tsaURL string) (runtime.Identity, error) {
	id := runtime.Identity{}
	if tsaURL != "" {
		var err error
		if id, err = runtime.ParseURLToIdentity(tsaURL); err != nil {
			return nil, fmt.Errorf("parsing TSA URL %q for identity: %w", tsaURL, err)
		}
	}
	id.SetType(tsaidentityv1alpha1.Type)
	return id, nil
}

// RootCertPoolFromCredentials loads the TSA root certificates from resolved
// credentials, accepting both the typed form and the DirectCredentials fallback.
// It returns nil, nil when no root certificates are configured.
func RootCertPoolFromCredentials(creds runtime.Typed) (*x509.CertPool, error) {
	typed, err := tsacredentialsv1alpha1.ConvertToTSACredentials(creds)
	if err != nil {
		return nil, fmt.Errorf("converting credentials: %w", err)
	}
	data, source, err := loadPEMBytes(typed)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("no valid certificates found in %s", source)
	}
	return pool, nil
}

func loadPEMBytes(creds *tsacredentialsv1alpha1.TSACredentials) ([]byte, string, error) {
	if creds.RootCertsPEM != "" {
		return []byte(creds.RootCertsPEM), "rootCertsPEM", nil
	}
	if path := creds.RootCertsPEMFile; path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("reading root certificates from %q: %w", path, err)
		}
		return data, "rootCertsPEMFile", nil
	}
	return nil, "", nil
}
