package attestation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	ocmblob "ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/cli/cmd/download/shared"
	cliattestation "ocm.software/open-component-model/bindings/go/cli/internal/attestation"
	"ocm.software/open-component-model/bindings/go/cli/internal/flags/enum"
	"ocm.software/open-component-model/bindings/go/cli/internal/render"
	"ocm.software/open-component-model/bindings/go/cli/internal/repository/ocm"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	FlagResourceIdentity = "identity"
	FlagType             = "type"
	FlagOutput           = "output"
	FlagConcurrencyLimit = "concurrency-limit"
	FlagRecursive        = "recursive"
	FlagNoColor          = "no-color"
	FlagContentDigest    = "content-digest"
)

// componentLevelLabel marks the group of referrers attached to the component version
// artifact itself, as opposed to a resource of the component. The full component:version
// name is shown in its own place (the COMPONENT column, or the tree root), so this label
// only needs to say "this is the component level".
const componentLevelLabel = "(component-level)"

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "attestation {reference}",
		Aliases: []string{"attestations", "att"},
		Short:   "Get the attestations describing a component version, its resources, or one resource",
		Args:    cobra.ExactArgs(1),
		Long: `Discover and list the attestations attached to a component version: SBOMs, VEX documents,
signatures, SLSA provenance, vulnerability reports, SARIF and anything else attached.

By default every attestation of the component version is listed: the component's own
referrers plus one group per resource of the component. Narrow the scope with:

  --identity name=X        only the attestations describing resource X.
  --content-digest DIGEST  only the attestation whose document bytes have this digest.
  --type TYPE              only attestations matching a friendly type or raw-type substring.
  --recursive              also follow componentReferences and list theirs too (-1 = unlimited).

Per resource, attestations are merged from every source:

  1. Resources stored in the component itself that declare, through the
     "ocm.software/artifact-references" label, that they describe the resource.
  2. OCI referrers attached to the resource's artifact (cosign, vexctl, oras, syft, notary, ...).
  3. BuildKit in-toto attestations embedded in the artifact's image index.

This is EXPERIMENTAL and read-only. The table and tree views list what exists from metadata
only, without downloading documents or verifying signatures. The json and yaml views instead
emit the documents themselves: each matching attestation is fetched and reduced to its decoded
inner payload (the real SBOM, VEX, provenance or other predicate, unwrapped from any in-toto
statement or Sigstore bundle), ready to pipe into another tool.

The tree nests the raw structure: the component or a resource, then the discovery source
(ocm-local, oci-referrer, oci-index), then each document. An enveloped document (an in-toto
statement or a Sigstore bundle) is a node named by its envelope media type, with its
predicate type(s) beneath it; a bare document is a leaf named by its media type. Each leaf
also carries a friendly TYPE tag (sbom, vex, signature, provenance, sarif) and the digest of
the document bytes. Documents with identical bytes attached in more than one place share that
digest and are tagged with the same letter, so
duplicates are easy to spot.

json emits an array of the decoded payloads (a bare object when a single document matches,
for example under --content-digest); yaml emits a stream separated by "---". Byte-identical
documents are emitted once; a document whose bytes are not JSON (a binary signature) is skipped.

Use --type to filter: it matches a substring of any raw type signal, plus friendly aliases
(for example "sbom" matches spdx and cyclonedx, "signature" matches cosign and notation).`,
		Example: `  # Every attestation of the component version and its resources
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0

  # As a provenance tree
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 -o tree

  # Only the "image" resource
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --identity name=image

  # Print one document, by its content digest (the decoded payload as json)
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --content-digest sha256:abc123... -o json

  # Follow componentReferences and list their attestations too
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --recursive

  # Every SBOM and VEX document, as a yaml stream
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --type sbom --type vex -o yaml`,
		RunE:              GetAttestations,
		DisableAutoGenTag: true,
	}

	cmd.Flags().String(FlagResourceIdentity, "", "list only the attestations describing this one resource")
	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum number of resources inspected in parallel when listing the whole component")
	cmd.Flags().Int(FlagRecursive, 0, "recursively follow componentReferences and list their attestations too; depth of recursion (-1 for unlimited)")
	cmd.Flags().Lookup(FlagRecursive).NoOptDefVal = "-1"
	cmd.Flags().StringSlice(FlagType, nil, "filter to attestations whose type contains any of these substrings or matches a friendly alias (sbom, vex, signature, provenance, spdx, ...); repeatable; empty lists all")
	cmd.Flags().String(FlagContentDigest, "", "filter to the attestation whose document content digest equals this value (for example sha256:...)")
	cmd.Flags().Bool(FlagNoColor, false, "disable coloured output (also honoured via the NO_COLOR environment variable)")
	enum.VarP(cmd.Flags(), FlagOutput, "o",
		[]string{
			render.OutputFormatTable.String(),
			render.OutputFormatJSON.String(),
			render.OutputFormatYAML.String(),
			render.OutputFormatTree.String(),
		},
		"output format")

	return cmd
}

func GetAttestations(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	pluginManager, credentialGraph, logger, err := shared.GetContextItems(cmd)
	if err != nil {
		return err
	}
	logger.Warn("get attestations is experimental: what is discovered, the columns shown and the flags themselves may change in a future release")

	identityStr, err := cmd.Flags().GetString(FlagResourceIdentity)
	if err != nil {
		return fmt.Errorf("getting identity flag failed: %w", err)
	}
	concurrency, err := cmd.Flags().GetInt(FlagConcurrencyLimit)
	if err != nil {
		return fmt.Errorf("getting concurrency-limit flag failed: %w", err)
	}
	typeFilters, err := cmd.Flags().GetStringSlice(FlagType)
	if err != nil {
		return fmt.Errorf("getting type flag failed: %w", err)
	}
	contentDigest, err := cmd.Flags().GetString(FlagContentDigest)
	if err != nil {
		return fmt.Errorf("getting content-digest flag failed: %w", err)
	}
	format, err := enum.Get(cmd.Flags(), FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}
	noColor, err := cmd.Flags().GetBool(FlagNoColor)
	if err != nil {
		return fmt.Errorf("getting no-color flag failed: %w", err)
	}
	recursive, err := cmd.Flags().GetInt(FlagRecursive)
	if err != nil {
		return fmt.Errorf("getting recursive flag failed: %w", err)
	}
	if identityStr != "" && recursive != 0 {
		return fmt.Errorf("--%s cannot be combined with --%s", FlagResourceIdentity, FlagRecursive)
	}
	switch {
	case recursive < -1:
		return fmt.Errorf("invalid recursion depth %d: must be -1 (unlimited) or >= 0", recursive)
	case recursive > 0:
		return fmt.Errorf("recursion depth > 0 is not implemented yet, use -1 for unlimited")
	}

	ref, err := compref.Parse(args[0])
	if err != nil {
		return fmt.Errorf("parsing component reference %q failed: %w", args[0], err)
	}
	repoProvider, err := ocm.NewComponentVersionRepositoryForComponentProvider(ctx, pluginManager.ComponentVersionRepositoryRegistry, credentialGraph, nil, ref)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repository: %w", err)
	}
	repo, err := repoProvider.GetComponentVersionRepositoryForComponent(ctx, ref.Component, ref.Version)
	if err != nil {
		return fmt.Errorf("could not access ocm repository: %w", err)
	}
	repo, desc, err := ocm.GetPinnedComponentVersion(ctx, repo, ref)
	if err != nil {
		return fmt.Errorf("getting component version failed: %w", err)
	}

	// For json and yaml the command emits the inner document of each attestation, so it
	// must fetch the document bytes; the tree and table views stay metadata-only.
	includeContent := format == render.OutputFormatJSON.String() || format == render.OutputFormatYAML.String()

	// walk resolves a component to its attestation groups and, when recursing, descends
	// into its componentReferences. A component already visited is emitted as a reference
	// marker (Repeated) without re-discovery, so diamonds and cycles terminate.
	visited := map[string]bool{}
	var walk func(repo repository.ComponentVersionRepository, desc *descriptor.Descriptor) (component, error)
	walk = func(repo repository.ComponentVersionRepository, desc *descriptor.Descriptor) (component, error) {
		ref := fmt.Sprintf("%s:%s", desc.Component.Name, desc.Component.Version)
		if visited[ref] {
			return component{Ref: ref, Repeated: true}, nil
		}
		visited[ref] = true

		req := cliattestation.Request{
			Descriptor:    desc,
			PluginManager: pluginManager,
			Credentials:   credentialGraph,
			Logger:        logger,
		}
		if ld, ok := repo.(repository.LocalAttestationDiscoverer); ok {
			req.DiscoverLocal = ld
		}
		if cd, ok := repo.(repository.ComponentAttestationDiscoverer); ok {
			req.DiscoverComponent = cd
		}
		if includeContent {
			req.IncludeData = true
			req.Download = func(ctx context.Context, res *descriptor.Resource, _ runtime.Identity) (ocmblob.ReadOnlyBlob, error) {
				return shared.DownloadResourceData(ctx, pluginManager, credentialGraph, desc, repo, res)
			}
		}
		groups, err := discoverGroups(ctx, req, desc, identityStr, concurrency)
		if err != nil {
			return component{}, err
		}
		node := component{Ref: ref, Groups: groups}

		if recursive == -1 {
			for i := range desc.Component.References {
				r := &desc.Component.References[i]
				childRepo, err := repoProvider.GetComponentVersionRepositoryForComponent(ctx, r.Component, r.Version)
				if err != nil {
					logger.Warn("resolving referenced component failed, skipping", slog.String("component", r.Component), slog.String("version", r.Version), slog.Any("error", err))
					continue
				}
				childRepo, childDesc, err := repository.GetPinnedComponentVersion(ctx, childRepo, r.Component, r.Version, nil)
				if err != nil {
					logger.Warn("getting referenced component version failed, skipping", slog.String("component", r.Component), slog.String("version", r.Version), slog.Any("error", err))
					continue
				}
				child, err := walk(childRepo, childDesc)
				if err != nil {
					logger.Warn("discovering attestations of referenced component failed, skipping", slog.String("component", r.Component), slog.String("version", r.Version), slog.Any("error", err))
					continue
				}
				node.Children = append(node.Children, child)
			}
		}
		return node, nil
	}

	root, err := walk(repo, desc)
	if err != nil {
		return err
	}
	root, _ = filterTree(root, typeFilters, contentDigest)
	return renderAttestations(ctx, cmd.OutOrStdout(), logger, format, root, colorEnabled(cmd.OutOrStdout(), noColor))
}

// discoverGroups resolves the selection to resource-labelled groups: --identity lists one
// resource; otherwise the component version artifact itself plus every resource.
func discoverGroups(ctx context.Context, baseReq cliattestation.Request, desc *descriptor.Descriptor, identityStr string, concurrency int) ([]cliattestation.Group, error) {
	if identityStr != "" {
		identity, err := runtime.ParseIdentity(identityStr)
		if err != nil {
			return nil, fmt.Errorf("parsing resource identity %q failed: %w", identityStr, err)
		}
		res, err := uniqueResource(desc, identity)
		if err != nil {
			return nil, err
		}
		req := baseReq
		req.Resource = res
		atts, err := cliattestation.Discover(ctx, req)
		if err != nil {
			return nil, err
		}
		return []cliattestation.Group{{Label: identityStr, Attestations: atts}}, nil
	}
	return cliattestation.DiscoverAll(ctx, baseReq, concurrency)
}

// filterGroups applies the type and content-digest filters within each group and drops
// groups left empty. A failed origin is kept so it stays visible regardless of filters.
func filterGroups(groups []cliattestation.Group, filters []string, contentDigest string) []cliattestation.Group {
	out := make([]cliattestation.Group, 0, len(groups))
	for _, g := range groups {
		if g.Err != nil {
			out = append(out, g) // keep a failed origin visible regardless of the filters
			continue
		}
		kept := filterByContentDigest(cliattestation.FilterByType(g.Attestations, filters), contentDigest)
		if len(kept) > 0 {
			out = append(out, cliattestation.Group{Label: g.Label, Attestations: kept})
		}
	}
	return out
}

// component is one node of the discovered tree: a component version, the attestation
// groups discovered for it (the component level plus one per resource), and its
// referenced components when recursing. Repeated marks a component already shown elsewhere
// in the tree (a diamond or cycle), rendered as a reference without re-discovery.
type component struct {
	Ref      string
	Groups   []cliattestation.Group
	Children []component
	Repeated bool
}

// filterTree applies the type and content-digest filters to every component in the tree,
// dropping components left with no attestations and no surviving descendants. The root is
// returned regardless so an empty result still renders. Repeated markers are kept as
// navigation aids even though they carry no attestations.
func filterTree(c component, filters []string, contentDigest string) (component, bool) {
	c.Groups = filterGroups(c.Groups, filters, contentDigest)
	kept := c.Children[:0]
	for _, child := range c.Children {
		if fc, ok := filterTree(child, filters, contentDigest); ok {
			kept = append(kept, fc)
		}
	}
	c.Children = kept
	return c, len(c.Groups) > 0 || len(c.Children) > 0 || c.Repeated
}

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

// filterByContentDigest keeps only attestations whose document content digest equals the
// given value. An empty filter keeps everything. The match is exact, so it selects every
// copy of one document wherever it is attached.
func filterByContentDigest(atts []repository.Attestation, digest string) []repository.Attestation {
	if digest == "" {
		return atts
	}
	kept := make([]repository.Attestation, 0, len(atts))
	for _, a := range atts {
		if a.ContentDigest == digest {
			kept = append(kept, a)
		}
	}
	return kept
}
