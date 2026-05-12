package download

import (
	"time"

	"helm.sh/helm/v4/pkg/getter"

	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

const (
	// CredentialCertFile is the key for storing the location of a client certificate.
	//
	// Deprecated: Use helmcredsv1.CredentialKeyCertFile instead.
	CredentialCertFile = helmcredsv1.CredentialKeyCertFile
	// CredentialKeyFile is the key for storing the location of a client private key.
	//
	// Deprecated: Use helmcredsv1.CredentialKeyKeyFile instead.
	CredentialKeyFile = helmcredsv1.CredentialKeyKeyFile
	// CredentialKeyring is the key for storing the keyring name to use.
	//
	// Deprecated: Use helmcredsv1.CredentialKeyKeyring instead.
	CredentialKeyring = helmcredsv1.CredentialKeyKeyring

	// DefaultHTTPTimeout
	// The cost timeout references curl's default connection timeout.
	// https://github.com/curl/curl/blob/master/lib/connect.h#L40C21-L40C21
	// The helm commands are usually executed manually. Considering the acceptable waiting time, we reduced the entire request time to 120s.
	DefaultHTTPTimeout = 120
)

var defaultOptions = []getter.Option{getter.WithTimeout(time.Second * DefaultHTTPTimeout)}

type option struct {
	//nolint:gocritic // Deprecated field is okay
	// Version is used in the following ways:
	// - in the case of HTTP/S based helm repositories, the version is ignored (e.g.: https://example.com/charts/mychart-1.2.3.tgz)
	// - in case of oci based helm repositories:
	//   - if not set, the version is taken from the oci reference tag (e.g.: oci://example.com/charts/mychart:1.2.3)
	//   - if set, and the oci reference tag is not set, the version is set to the value of this field.
	//   - if both are set they MUST equal otherwise helm download will fail with a mismatch error.
	// Deprecated: This field should NOT be used, either rely on the full http/s url or an oci reference with a tag.
	Version string `json:"version,omitempty"`

	//nolint:gocritic // Deprecated field is okay
	// CACert is used in combination with HelmRepository to specify a TLS root certificate to access the source helm repository.
	// Deprecated: This field is deprecated in favor of using certificates through the credentials.
	CACert string `json:"caCert,omitempty"`

	//nolint:gocritic // Deprecated field is okay
	// CACertFile is used in combination with HelmRepository to specify a relative filename
	// for TLS root certificate to access the source helm repository.
	// Deprecated: This field is deprecated in favor of using certificates through the credentials.
	CACertFile string `json:"caCertFile,omitempty"`

	// Credentials contains typed Helm credentials used for authentication and TLS configuration
	// when downloading charts from remote repositories.
	Credentials *helmcredsv1.HelmHTTPCredentials

	// OCICredentials contains credentials for direct OCI access instead of going through the Helm registry client.
	// This is used for OCI-based Helm repositories and allows for more direct control over OCI interactions.
	OCICredentials *ocicredsv1.OCICredentials

	// AlwaysDownloadProv indicates whether to always download the provenance file for the chart.
	// In cases where a Keyring is present in the credentials, Helm will attempt to download the provenance file to verify the chart's integrity.
	AlwaysDownloadProv bool `json:"alwaysDownloadProv,omitempty"`
}

// Option configures the behavior of [NewReadOnlyChartFromRemote].
type Option func(t *option)

// WithVersion sets an explicit chart version for the download.
//
// Deprecated: This option should not be used. Instead, rely on the full HTTP/S URL
// or an OCI reference with a tag to specify the version.
func WithVersion(version string) Option {
	return func(t *option) {
		t.Version = version
	}
}

// WithCACert sets an inline PEM-encoded CA certificate for TLS verification
// when connecting to the Helm repository.
//
// Deprecated: Use certificates through the credentials instead.
func WithCACert(caCert string) Option {
	return func(t *option) {
		t.CACert = caCert
	}
}

// WithCACertFile sets the path to a PEM-encoded CA certificate file for TLS verification
// when connecting to the Helm repository.
//
// Deprecated: Use certificates through the credentials instead.
func WithCACertFile(caCertFile string) Option {
	return func(t *option) {
		t.CACertFile = caCertFile
	}
}

// WithCredentials sets the typed Helm credentials used for authentication and TLS configuration.
func WithCredentials(credentials *helmcredsv1.HelmHTTPCredentials) Option {
	return func(t *option) {
		t.Credentials = credentials
	}
}

// WithOCICredentials sets the typed Helm credentials used for authentication against an OCI registry.
func WithOCICredentials(credentials *ocicredsv1.OCICredentials) Option {
	return func(t *option) {
		t.OCICredentials = credentials
	}
}

// WithAlwaysDownloadProv controls whether the provenance (.prov) file is always downloaded
// alongside the chart. When a keyring is provided via [CredentialKeyring], Helm will
// additionally attempt to verify the chart's integrity using the provenance file.
func WithAlwaysDownloadProv(dl bool) Option {
	return func(t *option) {
		t.AlwaysDownloadProv = dl
	}
}
