package shared

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
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

// DownloadResourceData downloads the content of res, a resource of desc, read through repo.
//
// The content is bound to the digest of res: resource repositories verify what they download
// against the resource they are handed, and component version repositories verify a local
// resource against the digest of the resource they return. That resource is read again by
// version, so it must record the digest res records; otherwise a version tag that moved since
// desc was resolved (or pinned) could substitute content.
func DownloadResourceData(ctx context.Context, pluginManager *manager.PluginManager, credentialGraph credentials.Resolver, desc *descriptor.Descriptor, repo repository.ComponentVersionRepository, res *descriptor.Resource, identity runtime.Identity) (blob.ReadOnlyBlob, error) {
	access := res.GetAccess()
	if v2.IsLocalBlob(access) || ociaccess.IsRelativeOCIReference(access) {
		name, version := desc.Component.Name, desc.Component.Version
		data, got, err := repo.GetLocalResource(ctx, name, version, identity)
		if err != nil {
			return nil, err
		}
		var gotDigest *descriptor.Digest
		if got != nil {
			gotDigest = got.Digest
		}
		if !sameDigest(gotDigest, res.Digest) {
			err := fmt.Errorf("local resource %q of %s:%s records digest %s, but the resolved component version records %s",
				res.Name, name, version, fmtDigest(gotDigest), fmtDigest(res.Digest))
			if closer, ok := data.(io.Closer); ok {
				err = errors.Join(err, closer.Close())
			}
			return nil, err
		}
		return data, nil
	}

	if access == nil {
		return nil, fmt.Errorf("access was empty for resource %s", res.Name)
	}
	plugin, err := pluginManager.ResourcePluginRegistry.GetResourcePlugin(ctx, access)
	if err != nil {
		return nil, fmt.Errorf("getting resource plugin for access %q failed: %w", access.GetType(), err)
	}
	var creds runtime.Typed
	if credIdentity, err := plugin.GetResourceCredentialConsumerIdentity(ctx, res); err == nil {
		if creds, err = credentialGraph.Resolve(ctx, credIdentity); err != nil && !errors.Is(err, credentials.ErrNotFound) {
			return nil, fmt.Errorf("getting credentials for resource %q failed: %w", res.Name, err)
		}
	}
	return plugin.DownloadResource(ctx, res, creds)
}

func sameDigest(a, b *descriptor.Digest) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func fmtDigest(d *descriptor.Digest) string {
	if d == nil {
		return "<none>"
	}
	return fmt.Sprintf("%s:%s (%s)", d.HashAlgorithm, d.Value, d.NormalisationAlgorithm)
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
