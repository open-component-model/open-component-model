package tsa

import (
	"crypto/x509"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// IdentityTypeTSA is the credential consumer identity type for RFC 3161
// Timestamping Authority configuration.
var IdentityTypeTSA = runtime.NewVersionedType("TSA", "v1alpha1")

// Credential property keys for TSA credentials.
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

	// CredentialKeyRootCertsPEM contains inline PEM-encoded root certificates
	// for verifying the TSA's PKCS#7 signature chain.
	CredentialKeyRootCertsPEM = "root_certs_pem"

	// CredentialKeyRootCertsPEMFile is a path to a PEM file containing root
	// certificates for verifying the TSA's PKCS#7 signature chain.
	CredentialKeyRootCertsPEMFile = CredentialKeyRootCertsPEM + "_file"
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
//   - type: Credentials/v1
//     properties:
//     root_certs_pem_file: /path/to/root-ca.pem
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
// It checks for inline PEM first (root_certs_pem), then a file path (root_certs_pem_file).
// Returns nil, nil if no root certificates are configured.
func RootCertPoolFromCredentials(credentials map[string]string) (*x509.CertPool, error) {
	data, err := loadPEMBytes(credentials)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("tsa: no valid certificates found in root_certs_pem")
	}
	return pool, nil
}

func loadPEMBytes(credentials map[string]string) ([]byte, error) {
	if val := credentials[CredentialKeyRootCertsPEM]; val != "" {
		return []byte(val), nil
	}
	if path := credentials[CredentialKeyRootCertsPEMFile]; path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("tsa: reading root certificates from %q: %w", path, err)
		}
		return data, nil
	}
	return nil, nil
}
