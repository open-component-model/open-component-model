// Package resource implements [repository.ResourceRepository] for Helm charts
// hosted in classic HTTP/HTTPS Helm repositories.
//
// The [ResourceRepository] downloads charts (and optional provenance files) from
// remote Helm repos and returns them as [blob.ChartBlob] values, which provide
// structured access to the chart .tgz and .prov entries.
//
// # Scope
//
// This repository handles the helm/v1 access type for classic (HTTP/HTTPS-based)
// Helm chart repositories only. Helm charts stored in OCI registries
// (oci:// scheme) should use the OCI ResourceRepository instead, which provides
// native OCI artifact handling including authentication and layer management.
//
// # Credentials
// TLS configuration (CA certificates, client certificates, private keys) should
// be provided through the credential resolver instead of being embedded in the
// access spec.
//
// # Usage
//
//	repo := resource.NewResourceRepository(filesystemConfig)
//
//	// Resolve credential consumer identity for a helm resource.
//	identity, err := repo.GetResourceCredentialConsumerIdentity(ctx, res)
//
//	// Download a chart from its remote helm repository.
//	chartBlob, err := repo.DownloadResource(ctx, res, creds)
//
// # Registration
//
// In the CLI the repository is registered as a builtin plugin:
//
//	manager.ResourcePluginRegistry.RegisterInternalResourcePlugin(
//	    resource.NewResourceRepository(filesystemConfig),
//	)
package resource
