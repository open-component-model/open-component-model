package resource

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	ocmblob "ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/cli/cmd/download/shared"
	"ocm.software/open-component-model/bindings/go/cli/internal/sbom"
)

// downloadContext defines all the items needed to fetch the SBOM.
type downloadContext struct {
	pluginManager   *manager.PluginManager
	credentialGraph credentials.Resolver
	logger          *slog.Logger
	ref             *compref.Ref
	repo            repository.ComponentVersionRepository
	descriptor      *descriptor.Descriptor
	resource        *descriptor.Resource
	output          string
	identity        runtime.Identity
}

// downloadSBOMs writes every SBOM describing the resource into a directory, one file
// each, and prints the paths written so they can be piped into a scanner.
func downloadSBOMs(cmd *cobra.Command, dc downloadContext) error {
	ctx := cmd.Context()

	dc.logger.Warn("--sbom is experimental: the set of SBOMs discovered, the layout written and the flags themselves may change in a future release")

	discovered, err := sbom.Discover(ctx, sbom.Request{
		Descriptor:    dc.descriptor,
		Resource:      dc.resource,
		PluginManager: dc.pluginManager,
		Credentials:   dc.credentialGraph,
		Logger:        dc.logger,
		Download: func(ctx context.Context, res *descriptor.Resource, identity runtime.Identity) (ocmblob.ReadOnlyBlob, error) {
			return shared.DownloadResourceData(ctx, dc.pluginManager, dc.credentialGraph, dc.ref.Component, dc.ref.Version, dc.repo, res, identity)
		},
		Options: []repository.SBOMOption{repository.WithAllSBOMPlatforms()},
	})
	if err != nil {
		return err
	}

	directory := dc.output
	if directory == "" {
		directory = sbom.Directory(dc.identity)
	}

	written, err := sbom.Write(discovered, directory)
	if err != nil {
		return err
	}

	dc.logger.Info("wrote discovered sboms",
		slog.String("resource", dc.resource.ToIdentity().String()),
		slog.String("directory", directory),
		slog.Int("documents", len(written)))

	for _, path := range written {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), path); err != nil {
			return fmt.Errorf("writing sbom output paths failed: %w", err)
		}
	}

	return nil
}
