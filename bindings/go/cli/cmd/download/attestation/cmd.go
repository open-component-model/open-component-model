package attestation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	ocmblob "ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/cli/cmd/download/shared"
	cliattestation "ocm.software/open-component-model/bindings/go/cli/internal/attestation"
	"ocm.software/open-component-model/bindings/go/cli/internal/repository/ocm"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	FlagResourceIdentity = "identity"
	FlagAllResources     = "all-resources"
	FlagType             = "type"
	FlagOutput           = "output"
	FlagConcurrencyLimit = "concurrency-limit"
	FlagRecursive        = "recursive"
)

// defaultOutputDir is the directory attestation documents are written into when --output
// is not given.
const defaultOutputDir = "attestations"

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "attestation {reference}",
		Aliases: []string{"attestations", "att"},
		Short:   "Download the attestation documents describing a component version, its resources, or one resource",
		Args:    cobra.ExactArgs(1),
		Long: `Download the attestation documents attached to a component version: SBOMs, VEX documents,
signatures, SLSA provenance, vulnerability reports, SARIF and anything else attached.

By default only the attestations attached to the component version artifact itself are downloaded.
Widen or narrow the scope with:

  --identity name=X   only the attestations describing resource X.
  --all-resources     every resource of the component plus the component's own referrers.
  --recursive         also follow componentReferences and download their attestations (-1 = unlimited).

Per resource, attestations are merged from every source:

  1. Resources stored in the component itself that declare, through the
     "ocm.software/artifact-references" label, that they describe the resource.
  2. OCI referrers attached to the resource's artifact (cosign, vexctl, oras, syft, notary, ...).
  3. BuildKit in-toto attestations embedded in the artifact's image index.

Each attestation's primary document is written to its own file in the output directory, byte for
byte as published, so digests and signatures over it still apply. The file name is a friendly label
derived from the type, a short id and a media-type extension; the paths written are printed to
standard output, one per line.

This is EXPERIMENTAL: what is discovered, how it is written out and the flags themselves may change
in a future release. Use --type to filter: it matches a substring of any raw type signal, plus
friendly aliases (for example "sbom" matches spdx and cyclonedx, "signature" matches cosign and
notation).`,
		Example: `  # The component's own referrers, into ./attestations
  ocm download attestation ghcr.io/acme//acme.org/app:1.0.0

  # Every resource plus the component's own referrers, into a chosen directory
  ocm download attestation ghcr.io/acme//acme.org/app:1.0.0 --all-resources --output ./atts

  # Only the "image" resource
  ocm download attestation ghcr.io/acme//acme.org/app:1.0.0 --identity name=image

  # Follow componentReferences and download their attestations too
  ocm download attestation ghcr.io/acme//acme.org/app:1.0.0 --all-resources --recursive

  # Only SBOMs
  ocm download attestation ghcr.io/acme//acme.org/app:1.0.0 --all-resources --type sbom`,
		RunE:              DownloadAttestations,
		DisableAutoGenTag: true,
	}

	cmd.Flags().String(FlagResourceIdentity, "", "download only the attestations describing this one resource")
	cmd.Flags().Bool(FlagAllResources, false, "download the attestations of every resource of the component plus the component's own referrers")
	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum number of resources inspected in parallel when downloading the whole component (--all-resources)")
	cmd.Flags().Int(FlagRecursive, 0, "recursively follow componentReferences and download their attestations too; depth of recursion (-1 for unlimited)")
	cmd.Flags().Lookup(FlagRecursive).NoOptDefVal = "-1"
	cmd.Flags().StringSlice(FlagType, nil, "filter to attestations whose type contains any of these substrings or matches a friendly alias (sbom, vex, signature, provenance, spdx, ...); repeatable; empty downloads all")
	cmd.Flags().StringP(FlagOutput, "o", defaultOutputDir, "output directory for the downloaded attestation documents")

	cmd.MarkFlagsMutuallyExclusive(FlagResourceIdentity, FlagAllResources)
	cmd.MarkFlagsMutuallyExclusive(FlagResourceIdentity, FlagRecursive)

	return cmd
}

func DownloadAttestations(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	pluginManager, credentialGraph, logger, err := shared.GetContextItems(cmd)
	if err != nil {
		return err
	}
	logger.Warn("download attestations is experimental: what is discovered, how it is written out and the flags themselves may change in a future release")

	flags, err := readFlags(cmd)
	if err != nil {
		return err
	}

	ref, err := compref.Parse(args[0])
	if err != nil {
		return fmt.Errorf("parsing component reference %q failed: %w", args[0], err)
	}
	provider, err := ocm.NewComponentVersionRepositoryForComponentProvider(ctx, pluginManager.ComponentVersionRepositoryRegistry, credentialGraph, nil, ref)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repository: %w", err)
	}
	repo, err := provider.GetComponentVersionRepositoryForComponent(ctx, ref.Component, ref.Version)
	if err != nil {
		return fmt.Errorf("could not access ocm repository: %w", err)
	}
	repo, desc, err := ocm.GetPinnedComponentVersion(ctx, repo, ref)
	if err != nil {
		return fmt.Errorf("getting component version failed: %w", err)
	}

	collector := &collector{
		pluginManager: pluginManager,
		credentials:   credentialGraph,
		logger:        logger,
		provider:      provider,
		flags:         flags,
		visited:       map[string]bool{},
	}
	all, err := collector.walk(ctx, repo, desc)
	if err != nil {
		return err
	}

	all = cliattestation.FilterByType(all, flags.typeFilters)
	withData := withDocuments(all, logger)
	unique, duplicates := cliattestation.Dedup(withData)
	for _, d := range duplicates {
		logger.Info("skipping duplicate attestation document, identical bytes already written",
			slog.String("type", d.Type()),
			slog.String("contentDigest", d.ContentDigest),
			slog.String("source", string(d.Source)),
			slog.String("id", d.ID))
	}

	written, err := cliattestation.Write(unique, flags.output)
	if err != nil {
		return err
	}

	logger.Info("wrote discovered attestations",
		slog.String("directory", flags.output),
		slog.Int("documents", len(written)),
		slog.Int("duplicatesSkipped", len(duplicates)))

	for _, path := range written {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), path); err != nil {
			return fmt.Errorf("writing attestation output paths failed: %w", err)
		}
	}

	return nil
}

// attestationFlags is the parsed, validated flag set of the command.
type attestationFlags struct {
	identity     string
	allResources bool
	concurrency  int
	typeFilters  []string
	recursive    int
	output       string
}

// readFlags reads and validates the command's flags.
func readFlags(cmd *cobra.Command) (attestationFlags, error) {
	var f attestationFlags
	var err error
	if f.identity, err = cmd.Flags().GetString(FlagResourceIdentity); err != nil {
		return f, fmt.Errorf("getting identity flag failed: %w", err)
	}
	if f.allResources, err = cmd.Flags().GetBool(FlagAllResources); err != nil {
		return f, fmt.Errorf("getting all-resources flag failed: %w", err)
	}
	if f.concurrency, err = cmd.Flags().GetInt(FlagConcurrencyLimit); err != nil {
		return f, fmt.Errorf("getting concurrency-limit flag failed: %w", err)
	}
	if f.typeFilters, err = cmd.Flags().GetStringSlice(FlagType); err != nil {
		return f, fmt.Errorf("getting type flag failed: %w", err)
	}
	if f.recursive, err = cmd.Flags().GetInt(FlagRecursive); err != nil {
		return f, fmt.Errorf("getting recursive flag failed: %w", err)
	}
	if f.output, err = cmd.Flags().GetString(FlagOutput); err != nil {
		return f, fmt.Errorf("getting output flag failed: %w", err)
	}
	switch {
	case f.recursive < -1:
		return f, fmt.Errorf("invalid recursion depth %d: must be -1 (unlimited) or >= 0", f.recursive)
	case f.recursive > 0:
		return f, fmt.Errorf("recursion depth > 0 is not implemented yet, use -1 for unlimited")
	}
	if f.output == "" {
		f.output = defaultOutputDir
	}
	return f, nil
}

// collector gathers a component's attestations at the selected scope and, when recursing,
// descends into its componentReferences, guarding against cycles.
type collector struct {
	pluginManager *manager.PluginManager
	credentials   credentials.Resolver
	logger        *slog.Logger
	provider      resolvers.ComponentVersionRepositoryResolver
	flags         attestationFlags
	visited       map[string]bool
}

// walk collects a component's attestations and, when recursing, those of its referenced
// components. A component already visited is skipped, so diamonds and cycles terminate.
// Resolving or discovering a referenced component is best-effort: a failure is logged and
// skipped, never fatal.
func (c *collector) walk(ctx context.Context, repo repository.ComponentVersionRepository, desc *descriptor.Descriptor) ([]repository.Attestation, error) {
	key := fmt.Sprintf("%s:%s", desc.Component.Name, desc.Component.Version)
	if c.visited[key] {
		return nil, nil
	}
	c.visited[key] = true

	all, err := c.collect(ctx, repo, desc)
	if err != nil {
		return nil, err
	}

	if c.flags.recursive != -1 {
		return all, nil
	}
	for i := range desc.Component.References {
		r := &desc.Component.References[i]
		child, err := c.walkReference(ctx, r)
		if err != nil {
			c.logger.Warn("downloading attestations of referenced component failed, skipping",
				slog.String("component", r.Component), slog.String("version", r.Version), slog.Any("error", err))
			continue
		}
		all = append(all, child...)
	}
	return all, nil
}

// walkReference resolves, pins and walks a single componentReference.
func (c *collector) walkReference(ctx context.Context, r *descriptor.Reference) ([]repository.Attestation, error) {
	childRepo, err := c.provider.GetComponentVersionRepositoryForComponent(ctx, r.Component, r.Version)
	if err != nil {
		return nil, fmt.Errorf("resolving referenced component failed: %w", err)
	}
	childRepo, childDesc, err := repository.GetPinnedComponentVersion(ctx, childRepo, r.Component, r.Version, nil)
	if err != nil {
		return nil, fmt.Errorf("getting referenced component version failed: %w", err)
	}
	return c.walk(ctx, childRepo, childDesc)
}

// collect discovers the attestations of one component at the selected scope, fetching each
// document's bytes. The Download closure binds to this component's repo and descriptor, so
// an ocm-local attestation's referenced resource is read correctly even when recursing.
func (c *collector) collect(ctx context.Context, repo repository.ComponentVersionRepository, desc *descriptor.Descriptor) ([]repository.Attestation, error) {
	base := cliattestation.Request{
		Descriptor:    desc,
		PluginManager: c.pluginManager,
		Credentials:   c.credentials,
		Logger:        c.logger,
		IncludeData:   true,
		Download: func(ctx context.Context, res *descriptor.Resource, _ runtime.Identity) (ocmblob.ReadOnlyBlob, error) {
			return shared.DownloadResourceData(ctx, c.pluginManager, c.credentials, desc, repo, res)
		},
	}
	if ld, ok := repo.(repository.LocalAttestationDiscoverer); ok {
		base.DiscoverLocal = ld
	}
	if cd, ok := repo.(repository.ComponentAttestationDiscoverer); ok {
		base.DiscoverComponent = cd
	}

	switch {
	case c.flags.identity != "":
		identity, err := runtime.ParseIdentity(c.flags.identity)
		if err != nil {
			return nil, fmt.Errorf("parsing resource identity %q failed: %w", c.flags.identity, err)
		}
		res, err := uniqueResource(desc, identity)
		if err != nil {
			return nil, err
		}
		base.Resource = res
		return cliattestation.Discover(ctx, base)
	case c.flags.allResources:
		groups, err := cliattestation.DiscoverAll(ctx, base, c.flags.concurrency)
		if err != nil {
			return nil, err
		}
		var out []repository.Attestation
		for _, g := range groups {
			out = append(out, g.Attestations...)
		}
		return out, nil
	default:
		return cliattestation.Discover(ctx, base)
	}
}

// withDocuments keeps the attestations whose document was fetched and reports the rest: an
// attestation with no Data is a source whose document could not be fetched, skipped rather
// than written as an empty file.
func withDocuments(atts []repository.Attestation, logger *slog.Logger) []repository.Attestation {
	out := make([]repository.Attestation, 0, len(atts))
	for _, a := range atts {
		if a.Data == nil {
			logger.Warn("attestation document could not be fetched, skipping",
				slog.String("type", a.Type()),
				slog.String("id", a.ID),
				slog.String("source", string(a.Source)))
			continue
		}
		out = append(out, a)
	}
	return out
}

// uniqueResource resolves an identity to exactly one resource of the descriptor, erroring
// when it matches none or several.
func uniqueResource(desc *descriptor.Descriptor, identity runtime.Identity) (*descriptor.Resource, error) {
	artifacts := make([]descriptor.Artifact, len(desc.Component.Resources))
	for i := range desc.Component.Resources {
		artifacts[i] = &desc.Component.Resources[i]
	}
	candidates := descriptor.FindArtifactsByIdentity(identity, artifacts)
	if len(candidates) != 1 {
		return nil, fmt.Errorf("expected exactly one resource matching identity %q, got %d", identity, len(candidates))
	}
	res, ok := candidates[0].(*descriptor.Resource)
	if !ok {
		return nil, fmt.Errorf("matched artifact is %T, not a resource", candidates[0])
	}
	return res, nil
}
