package attestation

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
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
)

// componentLabel is how the component version artifact itself is labelled in the output,
// distinct from any resource name.
const componentLabel = "(component)"

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

Per resource, attestations are merged from every source:

  1. Resources stored in the component itself that declare, through the
     "ocm.software/artifact-references" label, that they describe the resource.
  2. OCI referrers attached to the resource's artifact (cosign, vexctl, oras, syft, notary, ...).
  3. BuildKit in-toto attestations embedded in the artifact's image index.

This is EXPERIMENTAL and read-only. It lists what exists from metadata only: it does not download
the documents or verify any signature, and it does not classify types into a fixed vocabulary. The
TYPE column shows the raw type signal verbatim (in-toto predicate type, else artifact type, else
media type). Use --type to filter: it matches a substring of any raw type signal, plus friendly
aliases (for example "sbom" matches spdx and cyclonedx, "signature" matches cosign and notation).`,
		Example: `  # The component's own referrers
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0

  # The component's own referrers, as a provenance tree
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 -o tree

  # Every resource plus the component's own referrers
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 -o widetree

  # Only the "image" resource
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --identity name=image

  # Only SBOMs and VEX, as JSON
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --type sbom --type vex -o json`,
		RunE:              GetAttestations,
		DisableAutoGenTag: true,
	}

	cmd.Flags().String(FlagResourceIdentity, "", "list only the attestations describing this one resource")
	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum number of resources inspected in parallel when listing the whole component (-o widetree)")
	cmd.Flags().StringSlice(FlagType, nil, "filter to attestations whose type contains any of these substrings or matches a friendly alias (sbom, vex, signature, provenance, spdx, ...); repeatable; empty lists all")
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

	localDiscoverer, _ := repo.(repository.LocalAttestationDiscoverer)
	componentDiscoverer, _ := repo.(repository.ComponentAttestationDiscoverer)
	baseReq := cliattestation.Request{
		Descriptor:        desc,
		PluginManager:     pluginManager,
		Credentials:       credentialGraph,
		Logger:            logger,
		DiscoverLocal:     localDiscoverer,
		DiscoverComponent: componentDiscoverer,
	}

	groups, err := discoverGroups(ctx, baseReq, desc, identityStr, format, concurrency)
	if err != nil {
		return err
	}

	componentRef := fmt.Sprintf("%s:%s", desc.Component.Name, desc.Component.Version)
	return renderAttestations(ctx, cmd.OutOrStdout(), format, componentRef, filterGroups(groups, typeFilters))
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
		kept := filterByType(g.Attestations, filters)
		if len(kept) > 0 {
			out = append(out, cliattestation.Group{Label: g.Label, Attestations: kept})
		}
	}
	return out
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

// row is the rendered projection of an Attestation. The table shows the curated columns;
// JSON and YAML carry the raw type signals, the creation timestamp and the full
// annotation map too.
type row struct {
	Resource      string            `json:"resource,omitempty"`
	Source        string            `json:"source"`
	Type          string            `json:"type"`
	ArtifactType  string            `json:"artifactType,omitempty"`
	PredicateType string            `json:"predicateType,omitempty"`
	MediaType     string            `json:"mediaType,omitempty"`
	CreatedAt     string            `json:"createdAt,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	Platform      string            `json:"platform,omitempty"`
	Subject       string            `json:"subject,omitempty"`
	ID            string            `json:"id"`
	// Derived facets for the table, kept out of json/yaml which carry the raw signals.
	Category string `json:"-"`
	Format   string `json:"-"`
	Envelope string `json:"-"`
}

func renderAttestations(ctx context.Context, w io.Writer, format, ref string, groups []cliattestation.Group) error {
	switch format {
	case render.OutputFormatTree.String(), render.OutputFormatWideTree.String():
		return renderTree(ctx, w, ref, groups)
	}

	var rows []row
	for _, g := range groups {
		for _, a := range g.Attestations {
			cat, fmtLabel := classify(a)
			rows = append(rows, row{
				Resource:      displayLabel(g.Label),
				Source:        string(a.Source),
				Type:          a.Type(),
				ArtifactType:  a.ArtifactType,
				PredicateType: a.PredicateType,
				MediaType:     a.MediaType,
				CreatedAt:     a.CreatedAt,
				Annotations:   a.Annotations,
				Platform:      a.Platform.String(),
				Subject:       a.Digest,
				ID:            a.ID,
				Category:      cat,
				Format:        fmtLabel,
				Envelope:      envelopeOf(a),
			})
		}
	}
	slices.SortFunc(rows, func(a, b row) int {
		return cmp.Or(
			cmp.Compare(a.Resource, b.Resource),
			cmp.Compare(a.Source, b.Source),
			cmp.Compare(a.Type, b.Type),
			cmp.Compare(a.Platform, b.Platform),
			cmp.Compare(a.ID, b.ID),
		)
	})

	switch format {
	case render.OutputFormatJSON.String():
		data, err := json.Marshal(rows, runtime.JSONOptions, jsontext.WithIndent("  "))
		if err != nil {
			return fmt.Errorf("marshalling attestations to json failed: %w", err)
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	case render.OutputFormatYAML.String():
		data, err := sigyaml.Marshal(rows)
		if err != nil {
			return fmt.Errorf("marshalling attestations to yaml failed: %w", err)
		}
		_, err = w.Write(data)
		return err
	default:
		return renderTable(w, rows)
	}
}

func renderTable(w io.Writer, rows []row) error {
	t := table.NewWriter()
	t.SetOutputMirror(w)
	t.AppendHeader(table.Row{"RESOURCE", "SOURCE", "CATEGORY", "FORMAT", "PLATFORM", "ENV", "ID"})
	for _, r := range rows {
		t.AppendRow(table.Row{orDash(r.Resource), r.Source, orDash(r.Category), orDash(r.Format), orDash(r.Platform), orDash(r.Envelope), r.ID})
	}
	style := table.StyleLight
	style.Options.DrawBorder = false
	t.SetStyle(style)
	t.Render()
	return nil
}

// renderTree prints the attestations as a provenance tree. Containment nests (reference
// -> origin -> source); the orthogonal facets of each document (category, format, platform,
// envelope) are columns, not nesting levels, so heterogeneous values never stack in a single
// column. Digests are shown in full, like "oras discover". The whole structure is produced
// by the vertex serializer from a single root vertex.
func renderTree(ctx context.Context, w io.Writer, ref string, groups []cliattestation.Group) error {
	graph := syncdag.NewSyncedDirectedAcyclicGraph[string]()
	if err := graph.WithWriteLock(func(g *dag.DirectedAcyclicGraph[string]) error {
		return g.AddVertex(ref, map[string]any{syncdag.AttributeValue: groups})
	}); err != nil {
		return fmt.Errorf("building attestation tree failed: %w", err)
	}

	renderer := tree.New(ctx, graph,
		tree.WithRoots(ref),
		tree.WithHeader[string]("CATEGORY", "FORMAT", "PLATFORM", "ENV", "ID"),
		tree.WithVertexSerializerFunc(func(v *dag.Vertex[string]) (tree.Row, error) {
			gs, ok := v.Attributes[syncdag.AttributeValue].([]cliattestation.Group)
			if !ok {
				return tree.Row{}, fmt.Errorf("vertex %q has no %s attribute of type %T", v.ID, syncdag.AttributeValue, gs)
			}
			return rootTreeRow(v.ID, gs), nil
		}),
	)
	return renderer.Render(ctx, w)
}

// treeColumns is the number of data columns (after the nesting column) of the tree layout.
const treeColumns = 5

// rootTreeRow nests reference -> origin -> source, each source holding a document leaf per
// attestation.
func rootTreeRow(ref string, groups []cliattestation.Group) tree.Row {
	children := make([]tree.Row, 0, len(groups))
	for _, g := range groups {
		children = append(children, groupRow(displayLabel(g.Label), g.Attestations))
	}
	return innerTreeRow(ref, children)
}

// groupRow nests one origin (a resource or the component) over its sources.
func groupRow(label string, atts []repository.Attestation) tree.Row {
	bySource := map[repository.AttestationSource][]repository.Attestation{}
	for _, a := range atts {
		bySource[a.Source] = append(bySource[a.Source], a)
	}
	keys := make([]string, 0, len(bySource))
	for s := range bySource {
		keys = append(keys, string(s))
	}
	slices.Sort(keys)
	sources := make([]tree.Row, 0, len(keys))
	for _, sk := range keys {
		sources = append(sources, innerTreeRow(sk, leafRows(bySource[repository.AttestationSource(sk)])))
	}
	return innerTreeRow(label, sources)
}

// leafRows renders one document leaf per attestation, sorted so multi-platform entries
// cluster: by platform, then category, then format, then digest.
func leafRows(atts []repository.Attestation) []tree.Row {
	sorted := slices.Clone(atts)
	slices.SortFunc(sorted, func(a, b repository.Attestation) int {
		ca, fa := classify(a)
		cb, fb := classify(b)
		return cmp.Or(
			cmp.Compare(a.Platform.String(), b.Platform.String()),
			cmp.Compare(ca, cb),
			cmp.Compare(fa, fb),
			cmp.Compare(a.ID, b.ID),
		)
	})
	rows := make([]tree.Row, 0, len(sorted))
	for _, a := range sorted {
		rows = append(rows, leafTreeRow(a))
	}
	return rows
}

// leafTreeRow is a document leaf: its facets fill the columns, ending in the full digest.
func leafTreeRow(a repository.Attestation) tree.Row {
	category, format := classify(a)
	return tree.Row{Cells: []string{orDash(category), orDash(format), orDash(a.Platform.String()), orDash(envelopeOf(a)), a.ID}}
}

// innerTreeRow is a containment node (reference, origin or source): its label sits in the
// first column, the rest stay empty, and its children hang below.
func innerTreeRow(label string, children []tree.Row) tree.Row {
	cells := make([]string, treeColumns)
	cells[0] = label
	return tree.Row{Cells: cells, Children: children}
}

func displayLabel(label string) string {
	if label == "" {
		return componentLabel
	}
	return label
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
