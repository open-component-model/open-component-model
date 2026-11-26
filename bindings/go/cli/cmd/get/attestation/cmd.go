package attestation

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	sigyaml "sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/cli/cmd/download/shared"
	cliattestation "ocm.software/open-component-model/bindings/go/cli/internal/attestation"
	"ocm.software/open-component-model/bindings/go/cli/internal/flags/enum"
	"ocm.software/open-component-model/bindings/go/cli/internal/render"
	"ocm.software/open-component-model/bindings/go/cli/internal/render/graph/tree"
	"ocm.software/open-component-model/bindings/go/cli/internal/repository/ocm"
	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
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

By default only the referrers attached to the component version artifact itself are listed.
Widen or narrow the scope with:

  --identity name=X   only the attestations describing resource X.
  -o widetree         every resource of the component plus the component's own referrers.
  --recursive         also follow componentReferences and list their attestations (-1 = unlimited).

Per resource, attestations are merged from every source:

  1. Resources stored in the component itself that declare, through the
     "ocm.software/artifact-references" label, that they describe the resource.
  2. OCI referrers attached to the resource's artifact (cosign, vexctl, oras, syft, notary, ...).
  3. BuildKit in-toto attestations embedded in the artifact's image index.

This is EXPERIMENTAL and read-only. It lists what exists from metadata only: it does not
download the documents or verify any signature.

The tree nests the raw structure: the component or a resource, then the discovery source
(ocm-local, oci-referrer, oci-index), then each document. An enveloped document (an in-toto
statement or a Sigstore bundle) is a node named by its envelope media type, with its
predicate type(s) beneath it; a bare document is a leaf named by its media type. Each leaf
also carries a friendly TYPE tag (sbom, vex, signature, provenance, sarif) and the digest of
the document bytes. Documents with identical bytes attached in more than one place share that
digest and are tagged with the same letter (and a matching grey tone when colour is on), so
duplicates are easy to spot.

Use --type to filter: it matches a substring of any raw type signal, plus friendly aliases
(for example "sbom" matches spdx and cyclonedx, "signature" matches cosign and notation).`,
		Example: `  # The component's own referrers
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0

  # The component's own referrers, as a provenance tree
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 -o tree

  # Every resource plus the component's own referrers
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 -o widetree

  # Only the "image" resource
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --identity name=image

  # Follow componentReferences and list their attestations too
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --recursive -o widetree

  # Only SBOMs and VEX, as JSON
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --type sbom --type vex -o json`,
		RunE:              GetAttestations,
		DisableAutoGenTag: true,
	}

	cmd.Flags().String(FlagResourceIdentity, "", "list only the attestations describing this one resource")
	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum number of resources inspected in parallel when listing the whole component (-o widetree)")
	cmd.Flags().Int(FlagRecursive, 0, "recursively follow componentReferences and list their attestations too; depth of recursion (-1 for unlimited)")
	cmd.Flags().Lookup(FlagRecursive).NoOptDefVal = "-1"
	cmd.Flags().StringSlice(FlagType, nil, "filter to attestations whose type contains any of these substrings or matches a friendly alias (sbom, vex, signature, provenance, spdx, ...); repeatable; empty lists all")
	cmd.Flags().Bool(FlagNoColor, false, "disable coloured output (also honoured via the NO_COLOR environment variable)")
	enum.VarP(cmd.Flags(), FlagOutput, "o",
		[]string{
			render.OutputFormatTable.String(),
			render.OutputFormatJSON.String(),
			render.OutputFormatYAML.String(),
			render.OutputFormatTree.String(),
			render.OutputFormatWideTree.String(),
		},
		"output format; tree lists the component's own referrers, widetree every resource too")

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
		groups, err := discoverGroups(ctx, req, desc, identityStr, format, concurrency)
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
	root, _ = filterTree(root, typeFilters)
	return renderAttestations(ctx, cmd.OutOrStdout(), format, root, colorEnabled(cmd.OutOrStdout(), noColor))
}

// discoverGroups resolves the selection to resource-labelled groups: --identity lists one
// resource; -o widetree lists every resource plus the component artifact; every other output
// lists only the component artifact's own referrers.
func discoverGroups(ctx context.Context, baseReq cliattestation.Request, desc *descriptor.Descriptor, identityStr, format string, concurrency int) ([]cliattestation.Group, error) {
	switch {
	case identityStr != "":
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
	case format == render.OutputFormatWideTree.String():
		return cliattestation.DiscoverAll(ctx, baseReq, concurrency)
	default:
		atts, err := cliattestation.Discover(ctx, baseReq)
		if err != nil {
			return nil, err
		}
		return []cliattestation.Group{{Attestations: atts}}, nil
	}
}

// filterGroups applies the type filter within each group and drops groups left empty.
func filterGroups(groups []cliattestation.Group, filters []string) []cliattestation.Group {
	out := make([]cliattestation.Group, 0, len(groups))
	for _, g := range groups {
		if g.Err != nil {
			out = append(out, g) // keep a failed origin visible regardless of the type filter
			continue
		}
		kept := filterByType(g.Attestations, filters)
		if len(kept) > 0 {
			out = append(out, cliattestation.Group{Label: g.Label, Attestations: kept})
		}
	}
	return out
}

// component is one node of the discovered tree: a component version, the attestation
// groups discovered for it (component-level plus, in widetree, one per resource), and its
// referenced components when recursing. Repeated marks a component already shown elsewhere
// in the tree (a diamond or cycle), rendered as a reference without re-discovery.
type component struct {
	Ref      string
	Groups   []cliattestation.Group
	Children []component
	Repeated bool
}

// filterTree applies the type filter to every component in the tree, dropping components
// left with no attestations and no surviving descendants. The root is returned regardless
// so an empty result still renders its header. Repeated markers are kept as navigation
// aids even though they carry no attestations.
func filterTree(c component, filters []string) (component, bool) {
	c.Groups = filterGroups(c.Groups, filters)
	kept := c.Children[:0]
	for _, child := range c.Children {
		if fc, ok := filterTree(child, filters); ok {
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

// Friendly type vocabulary and envelope markers, inlined because the unopinionated model
// defines no type constants of its own. Used only to widen --type matching and to shape
// the provenance tree, never to classify or drop.
const (
	predicateOpenVEX       = "https://openvex.dev/ns"
	predicateSLSA          = "https://slsa.dev/provenance/"
	predicateCosignSig     = "https://sigstore.dev/cosign/sign/v1"
	mediaOpenVEX           = "application/openvex+json"
	mediaOpenVEXVnd        = "application/vnd.openvex+json"
	mediaSARIF             = "application/sarif+json"
	mediaInTotoStatement   = "application/vnd.in-toto+json"
	artifactCosignSig      = "application/vnd.dev.cosign.artifact.sig.v1+json"
	artifactNotation       = "application/vnd.cncf.notary.signature"
	artifactSigstoreBundle = "application/vnd.dev.sigstore.bundle.v0.3+json"
	annotationDSSEContent  = "dev.sigstore.bundle.content"
	resourceTypeSBOM       = "sbom"
	resourceTypeVEX        = "vex"
)

// classify derives the friendly category and format of an attestation from its raw type
// signals. It is advisory display sugar for the tree and the --type aliases, never stored
// and never a gate. An unrecognised attestation yields an empty category and its raw type
// as the format, so nothing is hidden.
func classify(a repository.Attestation) (category, format string) {
	pt, at, mt := a.PredicateType, a.ArtifactType, a.MediaType
	switch {
	case pt == repository.PredicateTypeSPDX || mt == repository.MediaTypeSPDXJSON:
		return "sbom", "spdx"
	case pt == repository.PredicateTypeCycloneDX || mt == repository.MediaTypeCycloneDXJSON:
		return "sbom", "cyclonedx"
	case strings.HasPrefix(pt, predicateOpenVEX) || mt == mediaOpenVEX || mt == mediaOpenVEXVnd:
		return "vex", "openvex"
	case strings.HasPrefix(pt, predicateSLSA):
		return "provenance", "slsa"
	case at == artifactCosignSig || pt == predicateCosignSig:
		return "signature", "cosign"
	case at == artifactNotation:
		return "signature", "notation"
	case at == mediaSARIF || mt == mediaSARIF:
		return "sarif", ""
	case at == resourceTypeSBOM:
		return "sbom", ""
	case at == resourceTypeVEX:
		return "vex", ""
	default:
		return "", a.Type()
	}
}

// typeAliases lists the friendly filter tokens of an attestation (its format and
// category), used only to widen --type matching. An unrecognised attestation yields none,
// staying matchable by raw substring.
func typeAliases(a repository.Attestation) []string {
	category, format := classify(a)
	var out []string
	if format != "" && format != a.Type() {
		out = append(out, format)
	}
	if category != "" {
		out = append(out, category)
	}
	return out
}

// envelopeOf names the wrapper a document sits in, from metadata alone: a cosign DSSE
// bundle, a plain in-toto statement, or none.
func envelopeOf(a repository.Attestation) string {
	switch {
	case a.ArtifactType == artifactSigstoreBundle || a.Annotations[annotationDSSEContent] == "dsse-envelope":
		return "dsse"
	case a.MediaType == mediaInTotoStatement:
		return "in-toto"
	default:
		return ""
	}
}

// filterByType keeps attestations whose raw type signals contain any of the given
// substrings (case-insensitive) or whose friendly aliases match one exactly. An empty
// filter keeps everything.
func filterByType(atts []repository.Attestation, filters []string) []repository.Attestation {
	if len(filters) == 0 {
		return atts
	}
	norm := make([]string, len(filters))
	for i, f := range filters {
		norm[i] = strings.ToLower(strings.TrimSpace(f))
	}
	kept := make([]repository.Attestation, 0, len(atts))
	for _, a := range atts {
		haystack := strings.ToLower(strings.Join([]string{a.Type(), a.ArtifactType, a.PredicateType, a.MediaType}, "\x00"))
		if anyFilterMatches(norm, haystack, typeAliases(a)) {
			kept = append(kept, a)
		}
	}
	return kept
}

func anyFilterMatches(filters []string, haystack string, aliases []string) bool {
	for _, f := range filters {
		if strings.Contains(haystack, f) || slices.Contains(aliases, f) {
			return true
		}
	}
	return false
}

// row is the table projection of one attestation. The table shows the raw type signals
// (media type, predicate type) plus the one friendly TYPE tag (the classified category),
// never an aggregated format or envelope. JSON and YAML use the nested model instead.
type row struct {
	Component     string
	Resource      string
	Source        string
	Type          string
	PredicateType string
	MediaType     string
	Platform      string
	ContentDigest string
	ID            string
	Error         string
}

func renderAttestations(ctx context.Context, w io.Writer, format string, root component, color bool) error {
	p := painter{on: color}
	marks := markDuplicates(root)
	switch format {
	case render.OutputFormatTree.String(), render.OutputFormatWideTree.String():
		return renderTree(ctx, w, root, p, marks)
	case render.OutputFormatJSON.String():
		data, err := json.Marshal(jsonComponent(root), runtime.JSONOptions, jsontext.WithIndent("  "))
		if err != nil {
			return fmt.Errorf("marshalling attestations to json failed: %w", err)
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	case render.OutputFormatYAML.String():
		data, err := sigyaml.Marshal(jsonComponent(root))
		if err != nil {
			return fmt.Errorf("marshalling attestations to yaml failed: %w", err)
		}
		_, err = w.Write(data)
		return err
	}

	// Show the COMPONENT column only when more than one component carries attestations,
	// i.e. under --recursive. For a single component the name lives in the component-level
	// RESOURCE cell instead, keeping the common case narrow.
	showComponent := componentsWithGroups(root) > 1
	rows := flattenRows(root, showComponent)
	slices.SortFunc(rows, func(a, b row) int {
		return cmp.Or(
			cmp.Compare(a.ContentDigest, b.ContentDigest),
			cmp.Compare(a.Component, b.Component),
			cmp.Compare(a.Resource, b.Resource),
			cmp.Compare(a.Source, b.Source),
			cmp.Compare(a.MediaType, b.MediaType),
			cmp.Compare(a.PredicateType, b.PredicateType),
			cmp.Compare(a.Platform, b.Platform),
			cmp.Compare(a.ID, b.ID),
		)
	})
	return renderTable(w, rows, showComponent, p, marks)
}

// componentsWithGroups counts the components in the tree that carry at least one
// attestation group, so the table can decide whether a COMPONENT column is warranted.
func componentsWithGroups(c component) int {
	n := 0
	if len(c.Groups) > 0 {
		n++
	}
	for _, child := range c.Children {
		n += componentsWithGroups(child)
	}
	return n
}

// flattenRows projects the component tree into table rows depth-first. Each row is tagged
// with its owning component; the RESOURCE cell is the resource identity, or, for the
// component-level group, the component:version name when no COMPONENT column carries it
// and (component-level) otherwise.
func flattenRows(c component, showComponent bool) []row {
	var rows []row
	for _, g := range c.Groups {
		for _, a := range g.Attestations {
			category, _ := classify(a)
			rows = append(rows, row{
				Component:     c.Ref,
				Resource:      resourceLabel(g.Label, c.Ref, showComponent),
				Source:        string(a.Source),
				Type:          category,
				PredicateType: a.PredicateType,
				MediaType:     a.MediaType,
				Platform:      a.Platform.String(),
				ContentDigest: a.ContentDigest,
				ID:            a.ID,
			})
		}
		if g.Err != nil {
			rows = append(rows, row{
				Component: c.Ref,
				Resource:  resourceLabel(g.Label, c.Ref, showComponent),
				Source:    "(unavailable)",
				Error:     g.Err.Error(),
			})
		}
	}
	for _, child := range c.Children {
		rows = append(rows, flattenRows(child, showComponent)...)
	}
	return rows
}

func renderTable(w io.Writer, rows []row, showComponent bool, p painter, marks dupMarks) error {
	t := table.NewWriter()
	t.SetOutputMirror(w)
	header := table.Row{"RESOURCE", "SOURCE", "TYPE", "MEDIA-TYPE", "PREDICATE-TYPE", "PLATFORM", "CONTENT-DIGEST"}
	if showComponent {
		header = append(table.Row{"COMPONENT"}, header...)
	}
	t.AppendHeader(header)
	for _, r := range rows {
		cells := table.Row{
			p.paint(orDash(r.Resource), text.Bold),
			r.Source,
			p.paint(orDash(r.Type), categoryColor(r.Type)),
			orDash(r.MediaType),
			orDash(r.PredicateType),
			orDash(r.Platform),
			contentDigestCell(r.ContentDigest, marks, p),
		}
		if r.Error != "" {
			cells = table.Row{
				p.paint(orDash(r.Resource), text.Bold),
				r.Source,
				"-", "-", "-", "-",
				p.paint("⚠ discovery failed: "+conciseError(r.Error), text.FgYellow),
			}
		}
		if showComponent {
			cells = append(table.Row{p.paint(r.Component, text.Bold)}, cells...)
		}
		t.AppendRow(cells)
	}
	style := table.StyleLight
	style.Options.DrawBorder = false
	t.SetStyle(style)
	t.Render()
	return nil
}

// renderTree prints the attestations as a pure provenance tree: component -> origin
// (component-level or a resource) -> source -> document. An enveloped document is a node
// named by its envelope media type with its predicate leaves below; a bare document is a
// leaf named by its media type. The only columns after the nesting are the friendly TYPE
// tag and the full content digest; there is no header. An empty result prints one line.
func renderTree(ctx context.Context, w io.Writer, root component, p painter, marks dupMarks) error {
	if len(root.Groups) == 0 && len(root.Children) == 0 && !root.Repeated {
		_, err := fmt.Fprintf(w, "no attestations found for %s\n", root.Ref)
		return err
	}
	graph := syncdag.NewSyncedDirectedAcyclicGraph[string]()
	if err := graph.WithWriteLock(func(g *dag.DirectedAcyclicGraph[string]) error {
		return g.AddVertex(root.Ref, map[string]any{syncdag.AttributeValue: root})
	}); err != nil {
		return fmt.Errorf("building attestation tree failed: %w", err)
	}
	// A pure provenance tree: no header, and no ● child indicator (every inner node is a
	// grouping, so marking them adds noise rather than meaning).
	style := tree.DefaultTreeStyle
	style.CharChildIndicator = ""
	renderer := tree.New(ctx, graph,
		tree.WithRoots(root.Ref),
		tree.WithoutHeader[string](),
		tree.WithTreeStyle[string](style),
		tree.WithVertexSerializerFunc(func(v *dag.Vertex[string]) (tree.Row, error) {
			c, ok := v.Attributes[syncdag.AttributeValue].(component)
			if !ok {
				return tree.Row{}, fmt.Errorf("vertex %q has no %s attribute of type %T", v.ID, syncdag.AttributeValue, c)
			}
			return componentTreeRow(c, p, marks), nil
		}),
	)
	return renderer.Render(ctx, w)
}

// componentTreeRow nests one component over its origins (component-level and, in widetree,
// one per resource) and then its referenced components. A Repeated component is a leaf
// marker pointing at its earlier appearance.
func componentTreeRow(c component, p painter, marks dupMarks) tree.Row {
	if c.Repeated {
		return nodeRow(c.Ref+"  (shown above)", nil, p)
	}
	children := make([]tree.Row, 0, len(c.Groups)+len(c.Children))
	for _, g := range c.Groups {
		children = append(children, originRow(g, p, marks))
	}
	for _, child := range c.Children {
		children = append(children, componentTreeRow(child, p, marks))
	}
	return nodeRow(c.Ref, children, p)
}

// originRow nests one origin (a resource or the component level) over its sources. A failed
// origin shows a single warning leaf naming the image that could not be discovered.
func originRow(g cliattestation.Group, p painter, marks dupMarks) tree.Row {
	label := treeGroupLabel(g.Label)
	if g.Err != nil {
		return nodeRow(label, []tree.Row{failedLeaf(g, p)}, p)
	}
	buckets := groupSources(g.Attestations)
	sources := make([]tree.Row, 0, len(buckets))
	for _, sb := range buckets {
		sources = append(sources, sourceRow(sb, p, marks))
	}
	return nodeRow(label, sources, p)
}

// sourceRow nests one source over its documents: an envelope node (named by the envelope
// media type) with its predicate leaves, or a bare document leaf.
func sourceRow(sb sourceBucket, p painter, marks dupMarks) tree.Row {
	children := make([]tree.Row, 0, len(sb.children))
	for _, ch := range sb.children {
		if ch.isRaw {
			children = append(children, leafRow(orDash(ch.raw.MediaType), ch.raw, p, marks))
			continue
		}
		preds := make([]tree.Row, 0, len(ch.predicates))
		for _, a := range ch.predicates {
			preds = append(preds, leafRow(predicateLabel(a), a, p, marks))
		}
		children = append(children, nodeRow(ch.mediaType, preds, p))
	}
	return nodeRow(sb.source, children, p)
}

// nodeRow is a containment node (component, origin, source or envelope): its bold label in
// the first column, the TYPE and CONTENT-DIGEST columns empty, children nested below.
func nodeRow(label string, children []tree.Row, p painter) tree.Row {
	return tree.Row{Cells: []string{p.paint(label, text.Bold), "", ""}, Children: children}
}

// leafRow is a document leaf: its label (media type for a bare document, predicate type
// with any platform for an enveloped one), the friendly TYPE tag coloured, and the content
// digest with its duplicate marker.
func leafRow(label string, a repository.Attestation, p painter, marks dupMarks) tree.Row {
	category, _ := classify(a)
	return tree.Row{Cells: []string{
		label,
		p.paint(category, categoryColor(category)),
		contentDigestCell(a.ContentDigest, marks, p),
	}}
}

// predicateLabel is the label of a predicate leaf under an envelope: its predicate type,
// with the platform appended in parentheses when the source carries one (so per-platform
// in-toto statements stay distinct).
func predicateLabel(a repository.Attestation) string {
	label := orDash(a.PredicateType)
	if plat := a.Platform.String(); plat != "" {
		label += " (" + plat + ")"
	}
	return label
}

// failedLeaf is the warning leaf of an origin whose discovery failed: it names the image
// reference that could not be reached (falling back to the ocm identity) and the concise
// reason. The full error stays in the WARN log and the json/yaml "error" field.
func failedLeaf(g cliattestation.Group, p painter) tree.Row {
	msg := "⚠ attestation discovery failed"
	if ref := cmp.Or(g.Reference, g.Label); ref != "" {
		msg += " for " + ref
	}
	if g.Err != nil {
		msg += ": " + conciseError(g.Err.Error())
	}
	return tree.Row{Cells: []string{p.paint(msg, text.FgYellow), "", ""}}
}

// sourceBucket is the documents of one source under an origin, as the ordered children the
// tree and the json both render: either an envelope node (predicates grouped by envelope
// media type) or a bare document leaf.
type sourceBucket struct {
	source   string
	children []sourceChild
}

// sourceChild is one child of a source: an envelope (predicates non-empty, named by
// mediaType) or a raw document leaf (isRaw, the attestation in raw).
type sourceChild struct {
	mediaType  string
	predicates []repository.Attestation
	raw        repository.Attestation
	isRaw      bool
}

// groupSources partitions a group's attestations by source, then within each source splits
// enveloped documents (envelopeOf != "", grouped under their envelope media type) from bare
// documents (a leaf each). It is the single grouping the tree and the json share, so the
// view and the machine output cannot disagree. Sources are sorted by name, children by
// media type, predicates by (type, predicate type, platform).
func groupSources(atts []repository.Attestation) []sourceBucket {
	bySource := map[string][]repository.Attestation{}
	var order []string
	for _, a := range atts {
		s := string(a.Source)
		if _, ok := bySource[s]; !ok {
			order = append(order, s)
		}
		bySource[s] = append(bySource[s], a)
	}
	slices.Sort(order)
	buckets := make([]sourceBucket, 0, len(order))
	for _, s := range order {
		envByType := map[string][]repository.Attestation{}
		var envOrder []string
		var children []sourceChild
		for _, a := range bySource[s] {
			if envelopeOf(a) != "" {
				label := cmp.Or(a.MediaType, a.ArtifactType)
				if _, ok := envByType[label]; !ok {
					envOrder = append(envOrder, label)
				}
				envByType[label] = append(envByType[label], a)
				continue
			}
			children = append(children, sourceChild{mediaType: a.MediaType, raw: a, isRaw: true})
		}
		for _, mt := range envOrder {
			preds := slices.Clone(envByType[mt])
			slices.SortFunc(preds, comparePredicates)
			children = append(children, sourceChild{mediaType: mt, predicates: preds})
		}
		slices.SortFunc(children, compareSourceChild)
		buckets = append(buckets, sourceBucket{source: s, children: children})
	}
	return buckets
}

func comparePredicates(a, b repository.Attestation) int {
	ca, _ := classify(a)
	cb, _ := classify(b)
	return cmp.Or(
		cmp.Compare(ca, cb),
		cmp.Compare(a.PredicateType, b.PredicateType),
		cmp.Compare(a.Platform.String(), b.Platform.String()),
		cmp.Compare(a.ID, b.ID),
	)
}

func compareSourceChild(a, b sourceChild) int {
	return cmp.Or(
		cmp.Compare(a.mediaType, b.mediaType),
		cmp.Compare(a.raw.ID, b.raw.ID),
	)
}

// jsonTree mirrors the tree for json/yaml: a component over its origins, each origin over
// its sources, each source over its documents. A document is either bare (its raw fields)
// or an envelope (enveloped true, named by its media type, with its predicates). Nothing
// aggregated is emitted beyond the one friendly "type" tag the view also shows.
type jsonTree struct {
	Component string       `json:"component"`
	Repeated  bool         `json:"repeated,omitzero"`
	Origins   []jsonOrigin `json:"origins,omitempty"`
	Children  []jsonTree   `json:"children,omitempty"`
}

type jsonOrigin struct {
	Origin    string       `json:"origin"`
	Reference string       `json:"reference,omitempty"`
	Error     string       `json:"error,omitempty"`
	Sources   []jsonSource `json:"sources,omitempty"`
}

type jsonSource struct {
	Source    string    `json:"source"`
	Documents []jsonDoc `json:"documents"`
}

type jsonDoc struct {
	Enveloped     bool              `json:"enveloped,omitzero"`
	Type          string            `json:"type,omitempty"`
	MediaType     string            `json:"mediaType,omitempty"`
	ArtifactType  string            `json:"artifactType,omitempty"`
	PredicateType string            `json:"predicateType,omitempty"`
	Platform      string            `json:"platform,omitempty"`
	Subject       string            `json:"subject,omitempty"`
	ContentDigest string            `json:"contentDigest,omitempty"`
	CreatedAt     string            `json:"createdAt,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	ID            string            `json:"id,omitempty"`
	Predicates    []jsonDoc         `json:"predicates,omitempty"`
}

// jsonComponent builds the nested machine model from the component tree, using the same
// groupSources partitioning as the tree view.
func jsonComponent(c component) jsonTree {
	out := jsonTree{Component: c.Ref, Repeated: c.Repeated}
	for _, g := range c.Groups {
		out.Origins = append(out.Origins, jsonOriginOf(g))
	}
	for _, child := range c.Children {
		out.Children = append(out.Children, jsonComponent(child))
	}
	return out
}

func jsonOriginOf(g cliattestation.Group) jsonOrigin {
	o := jsonOrigin{Origin: treeGroupLabel(g.Label), Reference: g.Reference}
	if g.Err != nil {
		o.Error = g.Err.Error()
		return o
	}
	for _, sb := range groupSources(g.Attestations) {
		s := jsonSource{Source: sb.source}
		for _, ch := range sb.children {
			if ch.isRaw {
				s.Documents = append(s.Documents, rawDoc(ch.raw))
				continue
			}
			doc := jsonDoc{Enveloped: true, MediaType: ch.mediaType}
			for _, a := range ch.predicates {
				doc.Predicates = append(doc.Predicates, predicateDoc(a))
			}
			s.Documents = append(s.Documents, doc)
		}
		o.Sources = append(o.Sources, s)
	}
	return o
}

func rawDoc(a repository.Attestation) jsonDoc {
	category, _ := classify(a)
	return jsonDoc{
		Type:          category,
		MediaType:     a.MediaType,
		ArtifactType:  a.ArtifactType,
		PredicateType: a.PredicateType,
		Platform:      a.Platform.String(),
		Subject:       a.Digest,
		ContentDigest: a.ContentDigest,
		CreatedAt:     a.CreatedAt,
		Annotations:   a.Annotations,
		ID:            a.ID,
	}
}

func predicateDoc(a repository.Attestation) jsonDoc {
	category, _ := classify(a)
	return jsonDoc{
		Type:          category,
		ArtifactType:  a.ArtifactType,
		PredicateType: a.PredicateType,
		Platform:      a.Platform.String(),
		Subject:       a.Digest,
		ContentDigest: a.ContentDigest,
		CreatedAt:     a.CreatedAt,
		Annotations:   a.Annotations,
		ID:            a.ID,
	}
}

// dupMark is the visible marker a content digest gets when it appears more than once: a
// short letter (the accessible primary cue) and an index into a grey ramp (reinforcement
// when colour is on). Singleton content gets no mark.
type dupMark struct {
	letter string
	grey   int
}

// dupMarks maps a content digest to its marker. Only digests that appear more than once
// are present.
type dupMarks map[string]dupMark

// markDuplicates walks the filtered tree and assigns a marker to every content digest that
// occurs more than once, in order of first appearance, so byte-identical documents attached
// in different places share a letter and colour across the whole view.
func markDuplicates(root component) dupMarks {
	counts := map[string]int{}
	var order []string
	var walk func(c component)
	walk = func(c component) {
		for _, g := range c.Groups {
			for _, a := range g.Attestations {
				if a.ContentDigest == "" {
					continue
				}
				if counts[a.ContentDigest] == 0 {
					order = append(order, a.ContentDigest)
				}
				counts[a.ContentDigest]++
			}
		}
		for _, child := range c.Children {
			walk(child)
		}
	}
	walk(root)

	marks := dupMarks{}
	next := 0
	for _, digest := range order {
		if counts[digest] < 2 {
			continue
		}
		marks[digest] = dupMark{letter: dupLetter(next), grey: next}
		next++
	}
	return marks
}

// dupLetter is the bijective base-26 label for index i: a, b, ..., z, aa, ab, ....
func dupLetter(i int) string {
	s := ""
	for i >= 0 {
		s = string(rune('a'+i%26)) + s
		i = i/26 - 1
	}
	return s
}

// greyRamp holds ANSI 256-colour grey indices cycled across duplicate groups. The letter
// stays the unambiguous marker, so repeating a grey for a later group is harmless.
var greyRamp = []int{250, 244, 248, 241, 246}

// contentDigestCell renders the CONTENT-DIGEST cell: a plain dimmed digest for unique
// content, or a letter-tagged, grey-toned digest when the same bytes appear elsewhere.
func contentDigestCell(digest string, marks dupMarks, p painter) string {
	if digest == "" {
		return p.paint("-", text.Faint)
	}
	if mark, dup := marks[digest]; dup {
		return p.grey("["+mark.letter+"] "+digest, mark.grey)
	}
	return p.paint(digest, text.Faint)
}

// conciseError is the root-cause tail of a wrapped error (the last ": "-delimited segment),
// so a long discovery error does not blow up a tree column or table cell. The full chain
// remains in the log and the machine-readable output.
func conciseError(msg string) string {
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		return msg[i+2:]
	}
	return msg
}

// resourceLabel is the RESOURCE cell for a group: the resource identity, or for the
// component-level group either the component:version name (when no COMPONENT column shows
// it) or the (component-level) marker (when it does).
func resourceLabel(groupLabel, ref string, showComponent bool) string {
	if groupLabel != "" {
		return groupLabel
	}
	if showComponent {
		return componentLevelLabel
	}
	return ref
}

// treeGroupLabel is the tree label for a group: the resource identity, or (component-level)
// for the component-level group (the component:version name is the parent node).
func treeGroupLabel(groupLabel string) string {
	if groupLabel == "" {
		return componentLevelLabel
	}
	return groupLabel
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// painter paints a cell with one or more ANSI attributes when enabled. The zero value
// (disabled) and every empty string pass through untouched, so the non-terminal and
// --no-color paths, and json/yaml, stay plain.
type painter struct{ on bool }

func (p painter) paint(s string, colors ...text.Color) string {
	if !p.on || s == "" || len(colors) == 0 {
		return s
	}
	return text.Colors(colors).Sprint(s)
}

// grey tints s with an ANSI 256-colour grey from greyRamp when colour is on, reinforcing
// the letter marker of a duplicate. Plain pass-through when disabled or empty.
func (p painter) grey(s string, index int) string {
	if !p.on || s == "" {
		return s
	}
	return fmt.Sprintf("\x1b[38;5;%dm%s\x1b[0m", greyRamp[index%len(greyRamp)], s)
}

// categoryColor maps a friendly attestation category to its display colour. Unknown or
// empty categories render faint, like the other secondary cells.
func categoryColor(category string) text.Color {
	switch category {
	case "sbom":
		return text.FgGreen
	case "signature":
		return text.FgCyan
	case "vex":
		return text.FgYellow
	case "provenance":
		return text.FgMagenta
	case "sarif":
		return text.FgRed
	default:
		return text.Faint
	}
}

// colorEnabled reports whether coloured output is wanted: only to a terminal, never when
// --no-color is set or the NO_COLOR environment variable is present.
func colorEnabled(w io.Writer, noColor bool) bool {
	return !noColor && os.Getenv("NO_COLOR") == "" && isTerminal(w)
}

// isTerminal reports whether the writer is a terminal (mirrors describe/types).
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
