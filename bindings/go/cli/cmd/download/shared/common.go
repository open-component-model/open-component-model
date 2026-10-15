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
	ocmctx "ocm.software/open-component-model/bindings/go/cli/internal/context"
	"ocm.software/open-component-model/bindings/go/cli/internal/flags/log"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
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

// DownloadResourceData downloads the content of res, a resource of desc, the component version
// ref names, read through repo. If ref is pinned to a digest, the content is verified against the
// digest of res (see VerifyResourceContent), so it is bound to the pinned descriptor also where it
// is not read from the pinned artifact (external accesses, or local blobs under a normalized pin).
func DownloadResourceData(ctx context.Context, pluginManager *manager.PluginManager, credentialGraph credentials.Resolver, ref *compref.Ref, desc *descriptor.Descriptor, repo repository.ComponentVersionRepository, res *descriptor.Resource, identity runtime.Identity) (blob.ReadOnlyBlob, error) {
	data, err := downloadResourceData(ctx, pluginManager, credentialGraph, desc.Component.Name, desc.Component.Version, repo, res, identity)
	if err != nil || ref.Digest == "" {
		return data, err
	}
	return VerifyResourceContent(ctx, res, data)
}

func downloadResourceData(ctx context.Context, pluginManager *manager.PluginManager, credentialGraph credentials.Resolver, component, version string, repo repository.ComponentVersionRepository, res *descriptor.Resource, identity runtime.Identity) (blob.ReadOnlyBlob, error) {
	access := res.GetAccess()
	var data blob.ReadOnlyBlob
	var err error

	if v2.IsLocalBlob(access) || ociaccess.IsRelativeOCIReference(access) {
		data, _, err = repo.GetLocalResource(ctx, component, version, identity)
	} else {
		if access == nil {
			return nil, fmt.Errorf("access was empty for resource %s", res.Name)
		}
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
