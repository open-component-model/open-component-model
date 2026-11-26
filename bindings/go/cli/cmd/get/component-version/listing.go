package componentversion

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/jedib0t/go-pretty/v6/table"
	"golang.org/x/sync/errgroup"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/cli/internal/render"
	"ocm.software/open-component-model/bindings/go/cli/internal/render/progress"
	ocm "ocm.software/open-component-model/bindings/go/cli/internal/repository/ocm"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

// listEventBufferSize buffers progress events so component workers do not block
// on the tracker.
const listEventBufferSize = 16

// phase reports progress for one concurrent listing phase. A nil phase (no
// tracker) is a no-op, so the collectors work with or without a UI.
type phase struct {
	events chan progress.Event[string]
	op     progress.Operation
}

// startPhase begins a tracker operation named name over total items. When
// tracker is nil it returns a no-op phase.
func startPhase(tracker *progress.Tracker[string], name string, total int) *phase {
	if tracker == nil {
		return &phase{}
	}
	events := make(chan progress.Event[string], listEventBufferSize)
	op := tracker.StartOperation(name,
		progress.WithEvents(events, func(e progress.Event[string]) progress.Event[string] { return e }, total),
		progress.WithConcurrency[string](listConcurrency))
	return &phase{events: events, op: op}
}

func (p *phase) emit(id string, state progress.State, err error) {
	if p.events != nil {
		p.events <- progress.Event[string]{ID: id, Name: id, State: state, Err: err}
	}
}

// finish closes the event channel and finishes the operation.
func (p *phase) finish() {
	if p.events != nil {
		close(p.events)
	}
	if p.op != nil {
		p.op.Finish(nil)
	}
}

// Detail levels for a repository reference. They trade completeness for cost:
// names needs only the catalog, versions adds one tag listing per component,
// and full fetches descriptors (and supports recursion and the tree outputs).
const (
	detailNames    = "names"
	detailVersions = "versions"
	detailFull     = "full"
)

// listConcurrency bounds the parallel per-component requests in the versions
// and full levels.
const listConcurrency = 4

// detailSelection carries the flag state that decides the detail level,
// so the decision is testable without a cobra command.
type detailSelection struct {
	detailSet     bool
	detail        string
	latestSet     bool
	constraintSet bool
	recursive     int
	output        string
	outputSet     bool
}

// effectiveDetailLevel returns the detail level to use for a repository
// reference. When --detail is not set, it picks the cheapest level that
// satisfies the other flags. When --detail is set but contradicts them, it
// returns an error before any network call.
func effectiveDetailLevel(sel detailSelection) (string, error) {
	// The default output format is the component graph (tree); it only forces
	// full when the user set it explicitly.
	outputNeedsFull := sel.outputSet && (sel.output == render.OutputFormatTree.String() || sel.output == render.OutputFormatWideTree.String())
	recursiveSet := sel.recursive != 0

	fullImplied := outputNeedsFull || recursiveSet
	versionsImplied := sel.latestSet || sel.constraintSet

	if sel.detailSet {
		switch sel.detail {
		case detailNames:
			if outputNeedsFull {
				return "", fmt.Errorf("--output %s needs --detail full", sel.output)
			}
			if recursiveSet {
				return "", fmt.Errorf("--recursive needs --detail full")
			}
			if sel.latestSet {
				return "", fmt.Errorf("--latest needs --detail versions or full")
			}
			if sel.constraintSet {
				return "", fmt.Errorf("--constraint needs --detail versions or full")
			}
		case detailVersions:
			if outputNeedsFull {
				return "", fmt.Errorf("--output %s needs --detail full", sel.output)
			}
			if recursiveSet {
				return "", fmt.Errorf("--recursive needs --detail full")
			}
		}
		return sel.detail, nil
	}

	switch {
	case fullImplied:
		return detailFull, nil
	case versionsImplied:
		return detailVersions, nil
	default:
		return detailNames, nil
	}
}

// effectiveComponentDetailLevel returns the detail level for a component
// reference (repo//comp). Unlike a repository reference, its default is full
// (today's behaviour), names is never valid, and a pinned reference (a version
// or a digest) can only be shown in full.
func effectiveComponentDetailLevel(sel detailSelection, pinned bool) (string, error) {
	if pinned {
		if sel.detailSet && sel.detail != detailFull {
			return "", fmt.Errorf("--detail %s cannot be used with a pinned component version: a pinned version has nothing to list; drop --detail or use --detail full", sel.detail)
		}
		return detailFull, nil
	}
	if !sel.detailSet {
		return detailFull, nil
	}
	switch sel.detail {
	case detailNames:
		return "", fmt.Errorf("--detail names needs a repository reference")
	case detailVersions:
		if sel.recursive != 0 {
			return "", fmt.Errorf("--recursive needs --detail full")
		}
		if sel.outputSet && (sel.output == render.OutputFormatTree.String() || sel.output == render.OutputFormatWideTree.String()) {
			return "", fmt.Errorf("--output %s needs --detail full", sel.output)
		}
		return detailVersions, nil
	default:
		return detailFull, nil
	}
}

// componentNameEntry is one row of the names output. It is an object, not a
// bare string, so fields can be added later without breaking consumers.
type componentNameEntry struct {
	Component string `json:"component"`
}

// componentVersionEntry is one row of the versions output.
type componentVersionEntry struct {
	Component string `json:"component"`
	Version   string `json:"version"`
}

// renderNames writes the component names in the requested output format.
func renderNames(w io.Writer, format string, names []string) error {
	switch format {
	case render.OutputFormatTable.String():
		t := table.NewWriter()
		t.SetOutputMirror(w)
		t.AppendHeader(table.Row{"Component"})
		for _, name := range names {
			t.AppendRow(table.Row{name})
		}
		setListTableStyle(t)
		t.Render()
		return nil
	default:
		entries := make([]componentNameEntry, 0, len(names))
		for _, name := range names {
			entries = append(entries, componentNameEntry{Component: name})
		}
		return writeStructured(w, format, entries)
	}
}

// renderVersions writes the component versions in the requested output format.
func renderVersions(w io.Writer, format string, entries []componentVersionEntry) error {
	switch format {
	case render.OutputFormatTable.String():
		t := table.NewWriter()
		t.SetOutputMirror(w)
		t.AppendHeader(table.Row{"Component", "Version"})
		for _, e := range entries {
			t.AppendRow(table.Row{e.Component, e.Version})
		}
		t.SetColumnConfigs([]table.ColumnConfig{{Number: 1, AutoMerge: true}})
		setListTableStyle(t)
		t.Render()
		return nil
	default:
		return writeStructured(w, format, entries)
	}
}

func setListTableStyle(t table.Writer) {
	style := table.StyleLight
	style.Options.DrawBorder = false
	t.SetStyle(style)
}

// writeStructured writes entries as JSON, NDJSON or YAML. It mirrors the
// encoders the graph renderer uses.
func writeStructured(w io.Writer, format string, entries any) error {
	switch format {
	case render.OutputFormatJSON.String():
		enc := jsontext.NewEncoder(w, jsontext.WithIndent("  "))
		return json.MarshalEncode(enc, entries, runtime.JSONOptions)
	case render.OutputFormatNDJSON.String():
		return writeNDJSON(w, entries)
	case render.OutputFormatYAML.String():
		data, err := yaml.Marshal(entries)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	default:
		return fmt.Errorf("invalid output format %q", format)
	}
}

// writeNDJSON writes one JSON object per line.
func writeNDJSON(w io.Writer, entries any) error {
	switch rows := entries.(type) {
	case []componentNameEntry:
		for _, e := range rows {
			if err := writeNDJSONLine(w, e); err != nil {
				return err
			}
		}
	case []componentVersionEntry:
		for _, e := range rows {
			if err := writeNDJSONLine(w, e); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported ndjson entry type %T", entries)
	}
	return nil
}

func writeNDJSONLine(w io.Writer, entry any) error {
	enc := jsontext.NewEncoder(w)
	return json.MarshalEncode(enc, entry, runtime.JSONOptions)
}

// collectComponentVersions lists the versions of each component with bounded
// concurrency (at most listConcurrency in flight). It skips a component that
// cannot be read, logging it once, and returns an error only when every
// component fails. Entries are sorted by component name, then version newest
// first using the registry. When tracker is non-nil, it reports one progress
// event per component.
func collectComponentVersions(ctx context.Context,
	tracker *progress.Tracker[string],
	repoProvider resolvers.ComponentVersionRepositoryResolver,
	componentNames []string,
	params Params,
	reg *versioning.Registry,
) ([]componentVersionEntry, error) {
	var (
		mu       sync.Mutex
		entries  []componentVersionEntry
		skipped  []string
		failures []error
	)

	skip := func(name string, err error) {
		mu.Lock()
		defer mu.Unlock()
		slog.ErrorContext(ctx, "skipping component that could not be read", "component", name, "error", err)
		skipped = append(skipped, name)
		failures = append(failures, fmt.Errorf("%s: %w", name, err))
	}

	p := startPhase(tracker, "Listing component versions", len(componentNames))

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(listConcurrency)
	for _, name := range componentNames {
		eg.Go(func() error {
			p.emit(name, progress.Running, nil)
			repo, err := repoProvider.GetComponentVersionRepositoryForComponent(egCtx, name, "")
			if err != nil {
				skip(name, err)
				p.emit(name, progress.Failed, err)
				return nil
			}
			versions, err := ocm.VersionsWithFiltering(egCtx, name, repo, ocm.VersionOptions{
				SemverConstraint: params.constraint,
				LatestOnly:       params.latestOnly,
				Registry:         reg,
			})
			if err != nil {
				skip(name, err)
				p.emit(name, progress.Failed, err)
				return nil
			}
			if len(versions) == 0 {
				err := errors.New("no matching versions")
				skip(name, err)
				p.emit(name, progress.Failed, err)
				return nil
			}
			mu.Lock()
			for _, v := range versions {
				entries = append(entries, componentVersionEntry{Component: name, Version: v})
			}
			mu.Unlock()
			p.emit(name, progress.Completed, nil)
			return nil
		})
	}
	// The per-component goroutines never return an error (failures are skipped),
	// so Wait only surfaces context cancellation.
	err := eg.Wait()
	p.finish()
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		if len(failures) > 0 {
			return nil, fmt.Errorf("no components in the repository could be read: %w", errors.Join(failures...))
		}
		return nil, nil
	}

	if len(skipped) > 0 {
		slog.WarnContext(ctx, fmt.Sprintf("skipped %d of %d components that could not be read", len(skipped), len(componentNames)), "skipped", skipped)
	}

	// Order by component name, then version newest first with the configured
	// registry, so the output is deterministic.
	var cmpErr error
	slices.SortStableFunc(entries, func(a, b componentVersionEntry) int {
		if a.Component != b.Component {
			return strings.Compare(a.Component, b.Component)
		}
		if cmpErr != nil {
			return 0
		}
		c, err := reg.Compare(b.Version, a.Version)
		if err != nil {
			cmpErr = err
			return 0
		}
		return c
	})
	if cmpErr != nil {
		return nil, fmt.Errorf("sorting component versions failed: %w", cmpErr)
	}

	return entries, nil
}
