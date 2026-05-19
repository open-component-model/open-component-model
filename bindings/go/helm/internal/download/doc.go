// Package download provides functionality for downloading Helm charts from remote repositories.
//
// It supports both OCI-based (oci://) and HTTP/S-based Helm repositories, handling
// authentication, TLS configuration, and provenance file downloads.
//
// # Basic Usage
//
// To download a chart from an OCI registry:
//
//	chartData, err := download.NewReadOnlyChartFromRemote(ctx, "oci://registry.example.com/charts/mychart:1.0.0")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// To download a chart from an HTTP/S repository:
//
//	chartData, err := download.NewReadOnlyChartFromRemote(ctx, "https://example.com/charts/mychart-1.0.0.tgz")
//
// # Authentication
//
// Credentials can be supplied via [WithCredentials] using typed [helmcredsv1.HelmHTTPCredentials]:
//
//	creds := &helmcredsv1.HelmHTTPCredentials{
//	    Username: "user",
//	    Password: "pass",
//	}
//	chartData, err := download.NewReadOnlyChartFromRemote(ctx, chartURL, download.WithCredentials(creds))
//
// For OCI-backed Helm repositories, credentials may additionally be supplied via
// [WithOCICredentials] using typed [ocicredsv1.OCICredentials]. Both option sets are
// then merged into a single basic-auth pair using the following resolution order:
//
//   - Username: [helmcredsv1.HelmHTTPCredentials.Username], falling back to
//     [ocicredsv1.OCICredentials.Username] when empty.
//   - Password: [helmcredsv1.HelmHTTPCredentials.Password], falling back to
//     [ocicredsv1.OCICredentials.Password], then to [ocicredsv1.OCICredentials.AccessToken]
//     (bearer/OAuth2 token) when each is empty.
//
// Basic auth is only attached to the request when both the resolved username and
// password are non-empty.
//
//	chartData, err := download.NewReadOnlyChartFromRemote(ctx, chartURL,
//	    download.WithCredentials(&helmcredsv1.HelmHTTPCredentials{Username: "user"}),
//	    download.WithOCICredentials(&ocicredsv1.OCICredentials{AccessToken: "token"}),
//	)
//
// # TLS Configuration
//
// Client certificates and CA certificates can be configured either inline or via file paths
// using [WithCACert], [WithCACertFile], or through credential keys [CredentialCertFile] and [CredentialKeyFile].
//
// # Provenance
//
// Provenance file downloading can be enabled with [WithAlwaysDownloadProv]. When a keyring is provided
// via the [CredentialKeyring] credential key, Helm will attempt to verify the chart's integrity.
package download
