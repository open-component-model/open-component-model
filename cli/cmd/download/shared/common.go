package shared

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/log"
)

// GetContextItems extracts common dependencies from cobra command
func GetContextItems(cmd *cobra.Command) (*manager.PluginManager, credentials.Resolver, *slog.Logger, error) {
	pluginManager := ocmctx.FromContext(cmd.Context()).PluginManager()
	if pluginManager == nil {
		return nil, nil, nil, fmt.Errorf("could not retrieve plugin manager from context")
	}

	credentialGraph := ocmctx.FromContext(cmd.Context()).CredentialGraph()
	if credentialGraph == nil {
		return nil, nil, nil, fmt.Errorf("could not retrieve credential graph from context")
	}

	logger, err := log.GetBaseLogger(cmd)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not retrieve logger: %w", err)
	}

	return pluginManager, credentialGraph, logger, nil
}

// DownloadResourceData handles the actual data download from repository
func DownloadResourceData(ctx context.Context, pluginManager *manager.PluginManager, credentialGraph credentials.Resolver, component, version string, repo repository.ComponentVersionRepository, res *descriptor.Resource, identity runtime.Identity) (blob.ReadOnlyBlob, error) {
	access := res.GetAccess()
	var data blob.ReadOnlyBlob
	var err error

	if IsLocal(access) {
		data, _, err = repo.GetLocalResource(ctx, component, version, identity)
	} else {
		var plugin resource.Repository
		plugin, err = pluginManager.ResourcePluginRegistry.GetResourcePlugin(ctx, access)
		if err != nil {
			return nil, fmt.Errorf("getting resource plugin for access %q failed: %w", access.GetType(), err)
		}
		var creds runtime.Typed
		if credIdentity, err := plugin.GetResourceCredentialConsumerIdentity(ctx, res); err == nil {
			if creds, err = credentialGraph.Resolve(ctx, credIdentity); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("getting credentials for resource %q failed: %w", res.Name, err)
			}
		}
		data, err = plugin.DownloadResource(ctx, res, creds)
	}

	return data, err
}

// SaveBlobToFile writes blob data to file with directory creation
func SaveBlobToFile(data blob.ReadOnlyBlob, outputPath string) error {
	// Ensure output directory exists
	outputDir := filepath.Dir(outputPath)
	if outputDir != "." && outputDir != "" {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("creating output directory %q failed: %w", outputDir, err)
		}
	}

	if err := filesystem.CopyBlobToOSPath(data, outputPath); err != nil {
		return fmt.Errorf("writing resource to %q failed: %w", outputPath, err)
	}
	return nil
}

// IsLocal checks if access method is local
func IsLocal(access runtime.Typed) bool {
	if access == nil {
		return false
	}
	var local v2.LocalBlob
	if err := v2.Scheme.Convert(access, &local); err != nil {
		return false
	}
	return true
}

// DownloadImageSBOMs resolves the resource plugin for the given resource and, if
// that plugin supports on-image SBOM discovery, returns any SBOM(s) attached to
// the resource's OCI image. If the resource is not backed by a plugin that
// supports on-image SBOM discovery (e.g. a local blob or a non-OCI access), it
// returns nil so callers can treat the absence as "no on-image SBOM".
func DownloadImageSBOMs(ctx context.Context, pluginManager *manager.PluginManager, credentialGraph credentials.Resolver, res *descriptor.Resource) ([]oci.SBOM, error) {
	access := res.GetAccess()
	if access == nil {
		return nil, nil
	}

	plugin, err := pluginManager.ResourcePluginRegistry.GetResourcePlugin(ctx, access)
	if err != nil {
		// No resource plugin handles this access type (e.g. LocalBlob). Such a
		// resource cannot carry an on-image SBOM, so this is not an error.
		return nil, nil
	}

	downloader, ok := plugin.(oci.ImageSBOMDownloader)
	if !ok {
		return nil, nil
	}

	var creds runtime.Typed
	if credIdentity, err := plugin.GetResourceCredentialConsumerIdentity(ctx, res); err == nil {
		if creds, err = credentialGraph.Resolve(ctx, credIdentity); err != nil && !errors.Is(err, credentials.ErrNotFound) {
			return nil, fmt.Errorf("getting credentials for resource %q failed: %w", res.Name, err)
		}
	}

	return downloader.DownloadImageSBOMs(ctx, res, creds)
}
