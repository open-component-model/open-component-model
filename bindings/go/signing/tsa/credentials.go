package tsa

import (
	"crypto/x509"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/runtime"
	tsacredentialsv1alpha1 "ocm.software/open-component-model/bindings/go/signing/tsa/spec/credentials/v1alpha1"
)

// IdentityTypeTSA is the credential consumer identity type for RFC 3161
// Timestamping Authority configuration.
var IdentityTypeTSA = runtime.NewVersionedType("TSA", "v1alpha1")

const (
	// TSAURLLabelPrefix is the label name prefix used to store the TSA URL
	// in a component descriptor. The full label name is the prefix followed by
	// the signature name (e.g. "url.tsa.ocm.software/default").
	TSAURLLabelPrefix = "url.tsa.ocm.software/"

	// VerifiedTimeKey is a well-known key in the credentials map that carries
	// the RFC 3339 formatted time from a verified TSA timestamp. When present
	// during PEM signature verification, it overrides the current time for X.509
	// certificate chain validation, allowing signatures to verify even when the
	// signing certificate has expired — provided the TSA timestamp proves the
	// signature was created while the certificate was still valid.
	VerifiedTimeKey = "tsa_verified_time"
)

// TSAConsumerIdentity builds a credential consumer identity for a TSA server.
// When a URL is provided, it is decomposed into the standard identity attributes
// (scheme, hostname, port, path) via runtime.ParseURLToIdentity, which enables
// matching via the credential graph's URL-based identity matching.
//
// Example .ocmconfig entry:
//
//   - identity:
//     type: TSA/v1alpha1
//     hostname: timestamp.sectigo.com
//     scheme: http
//     credentials:
//   - type: TSACredentials/v1alpha1
//     rootCertsPEMFile: /path/to/root-ca.pem
func TSAConsumerIdentity(tsaURL string) (runtime.Identity, error) {
	if tsaURL != "" {
		id, err := runtime.ParseURLToIdentity(tsaURL)
		if err != nil {
			return nil, fmt.Errorf("tsa: parsing TSA URL %q for identity: %w", tsaURL, err)
		}
		id.SetType(IdentityTypeTSA)
		return id, nil
	}
	id := runtime.Identity{}
	id.SetType(IdentityTypeTSA)
	return id, nil
}

// RootCertPoolFromCredentials loads an x509.CertPool from resolved TSA credentials.
// The credentials are converted to a typed [tsacredentialsv1alpha1.TSACredentials]
// (accepting both the typed form and the untyped DirectCredentials fallback), and
// its root certificates are loaded: inline PEM (rootCertsPEM) first, then a file
// path (rootCertsPEMFile). Returns nil, nil when no root certificates are configured.
func RootCertPoolFromCredentials(creds runtime.Typed) (*x509.CertPool, error) {
	typed, err := tsacredentialsv1alpha1.ConvertToTSACredentials(creds)
	if err != nil {
		return nil, fmt.Errorf("tsa: converting credentials: %w", err)
	}
	return RootCertPool(typed)
}

// RootCertPool loads an x509.CertPool from typed TSA credentials. It checks for
// inline PEM first (RootCertsPEM), then a file path (RootCertsPEMFile). Returns
// nil, nil when no root certificates are configured.
func RootCertPool(creds *tsacredentialsv1alpha1.TSACredentials) (*x509.CertPool, error) {
	if creds == nil {
		return nil, nil
	}
	data, source, err := loadPEMBytes(creds)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("tsa: no valid certificates found in %s", source)
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
			return nil, "", fmt.Errorf("tsa: reading root certificates from %q: %w", path, err)
		}
		return data, "rootCertsPEMFile", nil
	}
	return nil, "", nil
}
