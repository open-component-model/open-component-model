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
//	defer os.RemoveAll(chartData.ChartDir)
//
// To download a chart from an HTTP/S repository:
//
//	chartData, err := download.NewReadOnlyChartFromRemote(ctx, "https://example.com/charts/mychart-1.0.0.tgz")
//
// # Authentication
//
// Credentials can be supplied via [WithCredentials] using a map of key-value pairs.
// Username/password authentication is supported through the standard OCI credential keys:
//
//	creds := map[string]string{
//	    "username": "user",
//	    "password": "pass",
//	}
//	chartData, err := download.NewReadOnlyChartFromRemote(ctx, chartURL, download.WithCredentials(creds))
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
