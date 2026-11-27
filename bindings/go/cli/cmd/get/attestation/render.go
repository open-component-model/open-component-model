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
	sigyaml "sigs.k8s.io/yaml"

	cliattestation "ocm.software/open-component-model/bindings/go/cli/internal/attestation"
	"ocm.software/open-component-model/bindings/go/cli/internal/render"
	"ocm.software/open-component-model/bindings/go/cli/internal/render/graph/tree"
	"ocm.software/open-component-model/bindings/go/cli/internal/render/terminal"
	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Envelope markers, inlined because the unopinionated model defines no type constants of
// its own. Used only to shape the provenance tree (envelopeOf), never to classify or drop.
const (
	mediaInTotoStatement   = "application/vnd.in-toto+json"
	artifactSigstoreBundle = "application/vnd.dev.sigstore.bundle.v0.3+json"
	annotationDSSEContent  = "dev.sigstore.bundle.content"
)

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

func renderAttestations(ctx context.Context, w io.Writer, logger *slog.Logger, format string, root component, color bool) error {
	p := painter{on: color}
	marks := markDuplicates(root)
	switch format {
	case render.OutputFormatTree.String():
		return renderTree(ctx, w, root, p, marks)
	case render.OutputFormatJSON.String(), render.OutputFormatYAML.String():
		return renderContent(w, logger, root, format)
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

// renderContent emits the decoded inner documents of every attestation in the filtered
// tree: the real SBOM, VEX, provenance and other predicates, stripped of the envelope they
// were published in. Byte-identical documents are emitted once. An attestation whose bytes
// could not be fetched, or carry no extractable JSON payload (a binary signature), is
// skipped with a warning so it never corrupts the machine-readable output.
func renderContent(w io.Writer, logger *slog.Logger, root component, format string) error {
	withData := make([]repository.Attestation, 0)
	for _, a := range collectAttestations(root) {
		if a.Data == nil {
			logger.Warn("attestation document could not be fetched, skipping",
				slog.String("type", a.Type()), slog.String("id", a.ID), slog.String("source", string(a.Source)))
			continue
		}
		withData = append(withData, a)
	}
	unique, _ := cliattestation.Dedup(withData)

	payloads := make([]jsontext.Value, 0, len(unique))
	for _, a := range unique {
		payload, ok := cliattestation.Payload(a)
		if !ok {
			logger.Warn("attestation document carries no extractable JSON payload, skipping",
				slog.String("type", a.Type()), slog.String("id", a.ID), slog.String("source", string(a.Source)))
			continue
		}
		payloads = append(payloads, payload)
	}
	return writePayloads(w, payloads, format)
}

// collectAttestations flattens every attestation across the component tree depth-first.
// Failed origins carry no attestations, so they contribute nothing.
func collectAttestations(c component) []repository.Attestation {
	var out []repository.Attestation
	for _, g := range c.Groups {
		out = append(out, g.Attestations...)
	}
	for _, child := range c.Children {
		out = append(out, collectAttestations(child)...)
	}
	return out
}

// writePayloads serializes the decoded documents. JSON writes an indented array, collapsing
// to a bare object for a single document and to [] for none. YAML writes a stream of
// documents separated by "---", one re-serialized document each and nothing for none.
func writePayloads(w io.Writer, payloads []jsontext.Value, format string) error {
	switch format {
	case render.OutputFormatJSON.String():
		var value any = payloads
		if len(payloads) == 1 {
			value = payloads[0]
		}
		data, err := json.Marshal(value, runtime.JSONOptions, jsontext.WithIndent("  "))
		if err != nil {
			return fmt.Errorf("marshalling attestation documents to json failed: %w", err)
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	case render.OutputFormatYAML.String():
		for i, payload := range payloads {
			if i > 0 {
				if _, err := fmt.Fprintln(w, "---"); err != nil {
					return err
				}
			}
			doc, err := sigyaml.JSONToYAML(payload)
			if err != nil {
				return fmt.Errorf("marshalling attestation document to yaml failed: %w", err)
			}
			if _, err := w.Write(doc); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported content format %q", format)
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
			category, _ := cliattestation.Classify(a)
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
	// A pure provenance tree: no header, but keep the default ● indicator on nodes with
	// children, matching the tree of `ocm get cv`.
	renderer := tree.New(ctx, graph,
		tree.WithRoots(root.Ref),
		tree.WithoutHeader[string](),
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
	category, _ := cliattestation.Classify(a)
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
	ca, _ := cliattestation.Classify(a)
	cb, _ := cliattestation.Classify(b)
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

// dupMark is the visible marker a content digest gets when it appears more than once: a
// short letter shared by every byte-identical copy. Singleton content gets no mark.
type dupMark struct {
	letter string
}

// dupMarks maps a content digest to its marker. Only digests that appear more than once
// are present.
type dupMarks map[string]dupMark

// markDuplicates walks the filtered tree and assigns a marker to every content digest that
// occurs more than once, in order of first appearance, so byte-identical documents attached
// in different places share a letter across the whole view.
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
		marks[digest] = dupMark{letter: dupLetter(next)}
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

// contentDigestCell renders the CONTENT-DIGEST cell: a dimmed digest, prefixed with a
// letter tag shared by every copy when the same bytes appear elsewhere.
func contentDigestCell(digest string, marks dupMarks, p painter) string {
	if digest == "" {
		return p.paint("-", text.Faint)
	}
	if mark, dup := marks[digest]; dup {
		return p.paint("["+mark.letter+"] "+digest, text.Faint)
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
	return !noColor && os.Getenv("NO_COLOR") == "" && terminal.IsTerminal(w)
}
