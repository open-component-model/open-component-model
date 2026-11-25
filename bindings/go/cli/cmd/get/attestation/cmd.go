package attestation

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"sort"
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
	FlagComponent        = "component"
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

By default every resource of the component is inspected and the referrers attached to the
component version artifact itself are added, grouped by origin. Narrow it with:

  --identity name=X   only the attestations describing resource X.
  --component         only the referrers attached to the component version artifact itself.

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
		Example: `  # Everything: every resource plus the component's own referrers
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0

  # As a provenance tree
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 -o tree

  # Only the "image" resource
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --identity name=image

  # Only the component's own referrers
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --component

  # Only SBOMs and VEX, as JSON
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --type sbom --type vex -o json`,
		RunE:              GetAttestations,
		DisableAutoGenTag: true,
	}

	cmd.Flags().String(FlagResourceIdentity, "", "list only the attestations describing this one resource; mutually exclusive with --component")
	cmd.Flags().Bool(FlagComponent, false, "list only the referrers attached to the component version artifact itself; mutually exclusive with --identity")
	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum number of resources inspected in parallel when listing the whole component")
	cmd.Flags().StringSlice(FlagType, nil, "filter to attestations whose type contains any of these substrings or matches a friendly alias (sbom, vex, signature, provenance, spdx, ...); repeatable; empty lists all")
	enum.VarP(cmd.Flags(), FlagOutput, "o",
		[]string{
			render.OutputFormatTable.String(),
			render.OutputFormatJSON.String(),
			render.OutputFormatYAML.String(),
			render.OutputFormatTree.String(),
			render.OutputFormatWideTree.String(),
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
	componentOnly, err := cmd.Flags().GetBool(FlagComponent)
	if err != nil {
		return fmt.Errorf("getting component flag failed: %w", err)
	}
	if identityStr != "" && componentOnly {
		return fmt.Errorf("use either --%s or --%s, not both", FlagResourceIdentity, FlagComponent)
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

	groups, err := discoverGroups(ctx, baseReq, desc, identityStr, componentOnly, concurrency)
	if err != nil {
		return err
	}

	return renderAttestations(ctx, cmd.OutOrStdout(), format, args[0], filterGroups(groups, typeFilters))
}

// discoverGroups resolves the three selection modes to resource-labelled groups:
// --component (only the component artifact), --identity (one resource), or the default
// (every resource plus the component artifact, discovered in parallel).
func discoverGroups(ctx context.Context, baseReq cliattestation.Request, desc *descriptor.Descriptor, identityStr string, componentOnly bool, concurrency int) ([]cliattestation.Group, error) {
	switch {
	case componentOnly:
		atts, err := cliattestation.Discover(ctx, baseReq)
		if err != nil {
			return nil, err
		}
		return []cliattestation.Group{{Attestations: atts}}, nil
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
	default:
		return cliattestation.DiscoverAll(ctx, baseReq, concurrency), nil
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

// typeAliases derives friendly filter tokens from an attestation's raw type signals:
// the concrete format (spdx, cyclonedx, openvex, slsa, cosign, notation, sarif) and the
// broader group it belongs to (sbom, vex, signature, provenance). It is additive and
// advisory, used only to widen --type matching, never to relabel or drop. Unknown
// signals yield no aliases; the attestation stays matchable by raw substring.
func typeAliases(a repository.Attestation) []string {
	pt, at, mt := a.PredicateType, a.ArtifactType, a.MediaType
	var out []string
	switch {
	case pt == repository.PredicateTypeSPDX || mt == repository.MediaTypeSPDXJSON:
		out = []string{"spdx", "sbom"}
	case pt == repository.PredicateTypeCycloneDX || mt == repository.MediaTypeCycloneDXJSON:
		out = []string{"cyclonedx", "sbom"}
	case strings.HasPrefix(pt, predicateOpenVEX) || mt == mediaOpenVEX || mt == mediaOpenVEXVnd:
		out = []string{"openvex", "vex"}
	case strings.HasPrefix(pt, predicateSLSA):
		out = []string{"slsa", "provenance"}
	case at == artifactCosignSig || pt == predicateCosignSig:
		out = []string{"cosign", "signature"}
	case at == artifactNotation:
		out = []string{"notation", "signature"}
	case at == mediaSARIF || mt == mediaSARIF:
		out = []string{"sarif"}
	}
	switch at {
	case resourceTypeSBOM:
		if !slicesContains(out, "sbom") {
			out = append(out, "sbom")
		}
	case resourceTypeVEX:
		if !slicesContains(out, "vex") {
			out = append(out, "vex")
		}
	}
	return out
}

func slicesContains(s []string, v string) bool {
	for _, e := range s {
		if e == v {
			return true
		}
	}
	return false
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
		if strings.Contains(haystack, f) || slicesContains(aliases, f) {
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
}

func renderAttestations(ctx context.Context, w io.Writer, format, ref string, groups []cliattestation.Group) error {
	switch format {
	case render.OutputFormatTree.String(), render.OutputFormatWideTree.String():
		return renderTree(ctx, w, format, ref, groups)
	}

	var rows []row
	for _, g := range groups {
		for _, a := range g.Attestations {
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
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		switch {
		case rows[i].Resource != rows[j].Resource:
			return rows[i].Resource < rows[j].Resource
		case rows[i].Source != rows[j].Source:
			return rows[i].Source < rows[j].Source
		case rows[i].Type != rows[j].Type:
			return rows[i].Type < rows[j].Type
		case rows[i].Platform != rows[j].Platform:
			return rows[i].Platform < rows[j].Platform
		default:
			return rows[i].ID < rows[j].ID
		}
	})

	switch format {
	case render.OutputFormatJSON.String():
		data, err := json.Marshal(rows, jsontext.WithIndent("  "))
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
	t.AppendHeader(table.Row{"RESOURCE", "SOURCE", "TYPE", "MEDIA-TYPE", "PLATFORM", "ID"})
	for _, r := range rows {
		t.AppendRow(table.Row{orDash(r.Resource), r.Source, r.Type, orDash(r.MediaType), orDash(r.Platform), r.ID})
	}
	style := table.StyleLight
	style.Options.DrawBorder = false
	t.SetStyle(style)
	t.Render()
	return nil
}

// renderTree prints the attestations as a provenance tree: reference -> origin (resource
// or the component) -> source -> [platform] -> envelope chain -> document. Digests are
// shown in full, like "oras discover". The whole nested structure is produced by the
// vertex serializer from a single root vertex.
func renderTree(ctx context.Context, w io.Writer, format, ref string, groups []cliattestation.Group) error {
	graph := syncdag.NewSyncedDirectedAcyclicGraph[string]()
	if err := graph.WithWriteLock(func(g *dag.DirectedAcyclicGraph[string]) error {
		return g.AddVertex(ref, map[string]any{syncdag.AttributeValue: groups})
	}); err != nil {
		return fmt.Errorf("building attestation tree failed: %w", err)
	}

	wide := format == render.OutputFormatWideTree.String()
	header := []string{"TYPE", "ID"}
	if wide {
		header = []string{"TYPE", "MEDIA-TYPE", "CREATED", "ID"}
	}

	renderer := tree.New(ctx, graph,
		tree.WithRoots(ref),
		tree.WithHeader[string](header...),
		tree.WithVertexSerializerFunc(func(v *dag.Vertex[string]) (tree.Row, error) {
			gs, _ := v.Attributes[syncdag.AttributeValue].([]cliattestation.Group)
			return rootTreeRow(v.ID, gs, wide), nil
		}),
	)
	return renderer.Render(ctx, w)
}

// rootTreeRow builds the full nested row tree: a child per origin group, then source,
// optional platform (oci-index), the envelope chain and the document leaf.
func rootTreeRow(ref string, groups []cliattestation.Group, wide bool) tree.Row {
	children := make([]tree.Row, 0, len(groups))
	for _, g := range groups {
		children = append(children, innerTreeRow(displayLabel(g.Label), sourceRows(g.Attestations, wide), wide))
	}
	return innerTreeRow(ref, children, wide)
}

func sourceRows(atts []repository.Attestation, wide bool) []tree.Row {
	bySource := map[repository.AttestationSource][]repository.Attestation{}
	for _, a := range atts {
		bySource[a.Source] = append(bySource[a.Source], a)
	}
	keys := make([]string, 0, len(bySource))
	for s := range bySource {
		keys = append(keys, string(s))
	}
	sort.Strings(keys)

	rows := make([]tree.Row, 0, len(keys))
	for _, sk := range keys {
		group := bySource[repository.AttestationSource(sk)]
		if sk == string(repository.AttestationSourceOCIIndex) && anyHasPlatform(group) {
			byPlatform := map[string][]repository.Attestation{}
			for _, a := range group {
				byPlatform[a.Platform.String()] = append(byPlatform[a.Platform.String()], a)
			}
			platformKeys := make([]string, 0, len(byPlatform))
			for p := range byPlatform {
				platformKeys = append(platformKeys, p)
			}
			sort.Strings(platformKeys)
			platformRows := make([]tree.Row, 0, len(platformKeys))
			for _, pk := range platformKeys {
				platformRows = append(platformRows, innerTreeRow(orDash(pk), envelopeRows(byPlatform[pk], wide), wide))
			}
			rows = append(rows, innerTreeRow(sk, platformRows, wide))
		} else {
			rows = append(rows, innerTreeRow(sk, envelopeRows(group, wide), wide))
		}
	}
	return rows
}

// envelopeRows renders one chain per attestation, sorted by type then ID. A plain blob is
// a single leaf; a DSSE bundle or in-toto statement deepens into its wrapper so the signed
// document's predicate stays visible (unlike oras discover, which collapses them).
func envelopeRows(atts []repository.Attestation, wide bool) []tree.Row {
	sorted := make([]repository.Attestation, len(atts))
	copy(sorted, atts)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Type() != sorted[j].Type() {
			return sorted[i].Type() < sorted[j].Type()
		}
		return sorted[i].ID < sorted[j].ID
	})
	rows := make([]tree.Row, 0, len(sorted))
	for _, a := range sorted {
		rows = append(rows, chainRow(a, wide))
	}
	return rows
}

// chainRow turns one attestation into its wrapper chain, the document leaf last. The leaf
// carries the media type, creation timestamp and digest; the wrapper nodes are structure.
func chainRow(a repository.Attestation, wide bool) tree.Row {
	labels := envelopeChain(a)
	node := leafTreeRow(labels[len(labels)-1], a, wide)
	for i := len(labels) - 2; i >= 0; i-- {
		node = innerTreeRow(labels[i], []tree.Row{node}, wide)
	}
	return node
}

// envelopeChain lists the wrapper labels from outermost to the document, collapsing
// trivial single-container cases (a plain blob is just its type).
func envelopeChain(a repository.Attestation) []string {
	switch {
	case a.ArtifactType == artifactSigstoreBundle || a.Annotations[annotationDSSEContent] == "dsse-envelope":
		return []string{a.ArtifactType, "dsse", cmp.Or(a.PredicateType, "attestation")}
	case a.MediaType == mediaInTotoStatement:
		labels := make([]string, 0, 3)
		if a.ArtifactType != "" && a.ArtifactType != a.MediaType {
			labels = append(labels, a.ArtifactType)
		}
		return append(labels, "in-toto", cmp.Or(a.PredicateType, a.MediaType))
	default:
		return []string{a.Type()}
	}
}

func anyHasPlatform(atts []repository.Attestation) bool {
	for _, a := range atts {
		if a.Platform.String() != "" {
			return true
		}
	}
	return false
}

// leafTreeRow is a document leaf: its cells carry the type label, and (in widetree) the
// media type and creation timestamp, plus the digest.
func leafTreeRow(label string, a repository.Attestation, wide bool) tree.Row {
	if wide {
		return tree.Row{Cells: []string{label, orDash(a.MediaType), orDash(a.CreatedAt), a.ID}}
	}
	return tree.Row{Cells: []string{label, a.ID}}
}

// innerTreeRow is a grouping node (reference, origin, source, platform or wrapper): its
// label sits in the first column, the other columns stay empty, children hang below.
func innerTreeRow(label string, children []tree.Row, wide bool) tree.Row {
	cells := []string{label, ""}
	if wide {
		cells = []string{label, "", "", ""}
	}
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
