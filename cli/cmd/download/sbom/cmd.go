package sbom

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	cyclonedx "github.com/CycloneDX/cyclonedx-go"
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	sbomassembly "ocm.software/open-component-model/bindings/go/sbom"
	"ocm.software/open-component-model/cli/cmd/download/shared"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagOutput    = "output"
	FlagRecursive = "recursive"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sbom <component-version>",
		Short: "Download an orchestrating SBOM for a component version",
		Args:  cobra.ExactArgs(1),
		Long: `Download an orchestrating Software Bill of Materials (SBOM) for a component version.

This command collects the baked SBOM of every resource in the given component version and assembles
them into a single hierarchical CycloneDX document. SBOMs are discovered at build time (by the SBoM/v1
input method or by adding a resource of type 'sbom' linked via the 'ocm.software/sbom' label) and
embedded as local blobs; this command performs a pure local read and never fetches SBOMs from a registry.

Discovered SPDX SBOMs are normalized to CycloneDX so the whole document is a single CycloneDX BOM.
Resources without a baked SBOM are skipped with a warning.

With --recursive, the orchestration also descends into referenced (child) component versions,
nesting their SBOMs under the parent.`,
		Example: ` # Orchestrating SBOM for a single component version
  ocm download sbom ghcr.io/org/component:v1 --output ./sbom.cdx.json

  # Include referenced child component versions
  ocm download sbom ghcr.io/org/component:v1 --recursive --output ./sbom.cdx.json`,
		RunE:              DownloadSBOM,
		DisableAutoGenTag: true,
	}

	cmd.Flags().StringP(FlagOutput, "o", "", "output file for the orchestrating SBOM (default: <component>-<version>.cdx.json)")
	cmd.Flags().Int(FlagRecursive, 0, "depth of recursion into referenced component versions (0=none, -1=unlimited, >0=levels (not implemented yet))")
	cmd.Flags().Lookup(FlagRecursive).NoOptDefVal = "-1"

	return cmd
}

func DownloadSBOM(cmd *cobra.Command, args []string) error {
	pluginManager, credentialGraph, logger, err := shared.GetContextItems(cmd)
	if err != nil {
		return err
	}

	output, err := cmd.Flags().GetString(FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}
	recursive, err := cmd.Flags().GetInt(FlagRecursive)
	if err != nil {
		return fmt.Errorf("getting recursive flag failed: %w", err)
	}
	if recursive > 0 {
		return fmt.Errorf("--recursive with a positive depth is not implemented yet; use 0 (none) or bare --recursive (unlimited)")
	}

	ref, err := compref.Parse(args[0])
	if err != nil {
		return fmt.Errorf("parsing component reference %q failed: %w", args[0], err)
	}

	repoProvider, err := ocm.NewComponentVersionRepositoryForComponentProvider(cmd.Context(), pluginManager.ComponentVersionRepositoryRegistry, credentialGraph, nil, ref)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repository: %w", err)
	}

	builder := &nodeBuilder{
		ctx:             cmd.Context(),
		pluginManager:   pluginManager,
		credentialGraph: credentialGraph,
		repoProvider:    repoProvider,
		logger:          logger,
		recurse:         recursive != 0,
		visited:         make(map[string]struct{}),
	}

	root, err := builder.build(ref.Component, ref.Version)
	if err != nil {
		return err
	}

	bom, err := sbomassembly.Orchestrate(root)
	if err != nil {
		return fmt.Errorf("assembling orchestrating SBOM failed: %w", err)
	}
	data, err := sbomassembly.Encode(bom)
	if err != nil {
		return err
	}

	if output == "" {
		output = filepath.Base(ref.Component) + "-" + ref.Version + ".cdx.json"
	}
	if err := shared.SaveBlobToFile(inmemory.New(bytes.NewReader(data)), output); err != nil {
		return fmt.Errorf("writing orchestrating SBOM failed: %w", err)
	}
	logger.Info("orchestrating SBOM written", slog.String("output", output))
	return nil
}

// nodeBuilder walks a component version (and, in recursive mode, its references)
// discovering each resource's SBOM and assembling a sbomassembly.Node tree.
type nodeBuilder struct {
	ctx             context.Context
	pluginManager   *manager.PluginManager
	credentialGraph credentials.Resolver
	repoProvider    resolvers.ComponentVersionRepositoryResolver
	logger          *slog.Logger
	recurse         bool
	visited         map[string]struct{}
}

func (b *nodeBuilder) build(component, version string) (*sbomassembly.Node, error) {
	key := component + ":" + version
	if _, seen := b.visited[key]; seen {
		return nil, nil
	}
	b.visited[key] = struct{}{}

	repo, err := b.repoProvider.GetComponentVersionRepositoryForComponent(b.ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("could not access ocm repository for %s:%s: %w", component, version, err)
	}
	desc, err := repo.GetComponentVersion(b.ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("getting component version %s:%s failed: %w", component, version, err)
	}

	node := &sbomassembly.Node{Component: component, Version: version}

	for i := range desc.Component.Resources {
		res := &desc.Component.Resources[i]
		// Skip SBOM resources themselves: they are the SBOMs of other resources
		// and are pulled in as linked SBOMs, not treated as subjects.
		if res.Type == descriptor.ResourceTypeSBOM {
			continue
		}
		resSBOMs, err := b.discoverResourceSBOMs(desc, repo, component, version, res)
		if err != nil {
			return nil, err
		}
		node.Resources = append(node.Resources, resSBOMs...)
	}

	if b.recurse {
		for i := range desc.Component.References {
			refv := &desc.Component.References[i]
			child, err := b.build(refv.Component, refv.Version)
			if err != nil {
				return nil, err
			}
			if child != nil {
				node.Children = append(node.Children, child)
			}
		}
	}

	return node, nil
}

// discoverResourceSBOMs finds the baked SBOM(s) for a single resource via the
// label-linked sbom resource(s). Each discovered blob is normalized to CycloneDX.
//
// SBOMs are discovered at build time by the SBoM/v1 input method (which bakes them
// as local blobs carrying the ocm.software/sbom label); this command performs a
// pure local read and does not fetch SBOMs from a registry.
func (b *nodeBuilder) discoverResourceSBOMs(desc *descriptor.Descriptor, repo repository.ComponentVersionRepository, component, version string, res *descriptor.Resource) ([]sbomassembly.ResourceSBOM, error) {
	target := res.ToIdentity()

	linked, err := descriptor.FindSBOMResources(desc, target)
	if err != nil {
		return nil, fmt.Errorf("finding linked SBOM for resource %q failed: %w", res.Name, err)
	}
	var out []sbomassembly.ResourceSBOM
	for i := range linked {
		sbomRes := &linked[i]
		data, err := shared.DownloadResourceData(b.ctx, b.pluginManager, b.credentialGraph, component, version, repo, sbomRes, sbomRes.ToIdentity())
		if err != nil {
			return nil, fmt.Errorf("downloading linked SBOM %q failed: %w", sbomRes.Name, err)
		}
		bom, err := normalizeBlob(data, mediaTypeOf(data))
		if err != nil {
			return nil, fmt.Errorf("normalizing linked SBOM %q failed: %w", sbomRes.Name, err)
		}
		out = append(out, sbomassembly.ResourceSBOM{ResourceName: res.Name, BOM: bom})
	}

	if len(out) == 0 {
		b.logger.Warn("no baked SBOM discovered for resource, skipping", slog.String("resource", res.Name))
	}
	return out, nil
}

// normalizeBlob reads a blob and converts it to a CycloneDX BOM.
func normalizeBlob(b blob.ReadOnlyBlob, mediaType string) (bom *cyclonedx.BOM, err error) {
	rc, err := b.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("reading SBOM blob failed: %w", err)
	}
	defer func() {
		err = errors.Join(err, rc.Close())
	}()
	return sbomassembly.NormalizeToCycloneDX(rc, mediaType)
}

// mediaTypeOf returns the media type of a blob when it advertises one.
func mediaTypeOf(b blob.ReadOnlyBlob) string {
	if mta, ok := b.(blob.MediaTypeAware); ok {
		if mt, known := mta.MediaType(); known {
			return mt
		}
	}
	return ""
}
