// Package resource implements [repository.ResourceRepository] for Helm charts
// hosted in classic HTTP/HTTPS or OCI-based Helm repositories.
//
// The [ResourceRepository] downloads charts (and optional provenance files) from
// remote Helm repos and returns them as [blob.ChartBlob] values, which provide
// structured access to the chart .tgz and .prov entries.
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
