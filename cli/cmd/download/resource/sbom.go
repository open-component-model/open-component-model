package resource

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	ocmblob "ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/download/shared"
	"ocm.software/open-component-model/cli/internal/sbom"
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
	sbomFormat      string
	identity        runtime.Identity
}

// downloadSBOMs combines every SBOM describing the resource into one document and
// writes it to either an output location or stdout.
func downloadSBOMs(cmd *cobra.Command, dc downloadContext) (err error) {
	ctx := cmd.Context()

	documents, err := sbom.Discover(ctx, sbom.Request{
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

	combined, err := sbom.Combine(documents, dc.resource, time.Now())
	if err != nil {
		return err
	}
	dc.logger.Info("combined discovered sboms into one document",
		slog.String("resource", dc.resource.ToIdentity().String()),
		slog.Int("documents", len(documents)),
		slog.String("format", dc.sbomFormat))

	if dc.output == "" {
		return sbom.Write(combined, dc.sbomFormat, cmd.OutOrStdout())
	}

	file, err := os.Create(dc.output)
	if err != nil {
		return fmt.Errorf("creating sbom output file %q failed: %w", dc.output, err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	if werr := sbom.Write(combined, dc.sbomFormat, file); werr != nil {
		return werr
	}
	dc.logger.Info("wrote combined sbom", slog.String("path", dc.output))
	return nil
}
