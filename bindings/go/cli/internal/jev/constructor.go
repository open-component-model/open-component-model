package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	slogctx "github.com/veqryn/slog-context"

	"ocm.software/open-component-model/bindings/go/cli/internal/jev/scanner"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ComponentReport records the classification outcome for one emitted component,
// for the command to print to the user.
type ComponentReport struct {
	Name          string
	Version       string
	Kind          string
	VersionSource string
	Provider      string
	Method        string   // "rules" | "ai"
	Notes         []string // rationale for the classification
	TODOs         []string // fields the user must verify (e.g. guessed image ref)
}

// Report aggregates per-component reports plus whether the repo fanned out.
type Report struct {
	Monorepo   bool
	Components []ComponentReport
}

// rawSpec builds a *runtime.Raw from a type-tagged spec map. The "type" key is
// parsed into Raw.Type and the data is canonicalized (runtime/raw.go).
func rawSpec(spec map[string]any) (*runtime.Raw, error) {
	b, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("marshaling spec: %w", err)
	}
	raw := &runtime.Raw{}
	if err := raw.UnmarshalJSON(b); err != nil {
		return nil, fmt.Errorf("building raw spec: %w", err)
	}
	return raw, nil
}

// mustLabel builds a constructor Label whose value is the JSON encoding of v.
// Label.Value is a json.RawMessage that must hold valid JSON (a quoted string
// for scalars, e.g. "stable"), so json.Marshal is used rather than the
// YAML-based MustAsRawMessage which would emit a bare unquoted scalar.
func mustLabel(name string, v any) constructorv1.Label {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("cannot JSON-encode label %q value %T: %v", name, v, err))
	}
	return constructorv1.Label{Name: name, Value: b}
}

var (
	stripGitRE      = regexp.MustCompile(`\.git$`)
	componentSlugRE = regexp.MustCompile(`github\.com[:/](.+?)(?:\.git)?$`)
)

// componentName derives the OCM component name from the origin URL, falling back
// to ocm.software/<repo-name>. Ported from the prototype _component_name.
func componentName(state *RepoState) string {
	if m := componentSlugRE.FindStringSubmatch(state.OriginURL); m != nil {
		return "github.com/" + m[1]
	}
	return "ocm.software/" + state.RepoName
}

// chartDir returns the directory holding the detected Chart.yaml, relative to
// the repo root, or "." It prefers the dominant helm candidate's directory from
// the composition (authoritative); when no composition is present it falls back
// to the detected Chart.yaml location.
func chartDir(state *RepoState, dec *Decision) string {
	if dec != nil && dec.comp != nil && dec.comp.Dominant.Kind == "helmChart" {
		return dec.comp.Dominant.Dir
	}
	charts := state.DetectedManifests["helm_chart"]
	if len(charts) == 0 {
		return "."
	}
	d := filepath.Dir(charts[0])
	if d == "" || d == "." {
		return "."
	}
	return filepath.ToSlash(d)
}

// imageReference returns the image reference for an ociImage resource and
// whether it had to be guessed. It prefers an image reference discovered from
// the repository (dec.ImageHints, populated by the scanners from values.yaml /
// Makefile); when the hint already carries a tag it is used verbatim, otherwise
// the resolved version is appended. With no hint it falls back to the
// conventional GHCR path derived from the origin slug, marked as a guess.
func imageReference(dec *Decision, slug, version string) (ref string, guessed bool) {
	if len(dec.ImageHints) > 0 {
		h := dec.ImageHints[0]
		if strings.Contains(h[strings.LastIndex(h, "/")+1:], ":") {
			return h, false
		}
		return h + ":" + version, false
	}
	return fmt.Sprintf("ghcr.io/%s:%s", slug, version), true
}

// lastPathSeg returns the final slash-delimited segment of a reference, used to
// test whether a tag (":") is present on the image name rather than the host.
func lastPathSeg(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

// shortRefName derives a short, stable resource-name suffix from an image
// reference: the repository's last path segment, without registry or tag.
func shortRefName(ref string) string {
	seg := ref
	if i := strings.LastIndex(seg, ":"); i > strings.LastIndex(seg, "/") {
		seg = seg[:i]
	}
	return lastPathSeg(seg)
}

// buildComponent assembles a single component from a classified state/decision.
// pathPrefix is prepended to every input path so a sub-deliverable's inputs
// resolve from the repo root (empty for a single-repo classification). version
// is the resolved version; versionSource records how it was resolved. Any field
// that had to be guessed (e.g. a published image reference) is recorded as a
// TODO on the returned report so the user knows exactly what to verify.
func buildComponent(ctx context.Context, state *RepoState, dec *Decision, version, versionSource, pathPrefix string) (constructorv1.Component, ComponentReport, error) {
	name := componentName(state)
	slug := strings.TrimPrefix(name, "github.com/")
	provider := dec.Provider
	if provider == "" {
		provider = "ocm.software"
	}
	var todos []string

	joinPath := func(sub string) string {
		if pathPrefix == "" {
			if sub == "" {
				return "."
			}
			return sub
		}
		if sub == "" || sub == "." {
			return pathPrefix
		}
		return filepath.ToSlash(filepath.Join(pathPrefix, sub))
	}

	var sources []constructorv1.Source
	if dec.SourceAccess == "github" && strings.HasPrefix(state.OriginURL, "http") && state.HeadCommit != "" {
		access, err := rawSpec(map[string]any{
			"type":    "GitHub/v1",
			"repoUrl": stripGitRE.ReplaceAllString(state.OriginURL, ""),
			"commit":  state.HeadCommit,
		})
		if err != nil {
			return constructorv1.Component{}, ComponentReport{}, err
		}
		sources = append(sources, constructorv1.Source{
			ElementMeta:   constructorv1.ElementMeta{ObjectMeta: constructorv1.ObjectMeta{Name: "source", Version: version}},
			Type:          "git",
			AccessOrInput: constructorv1.AccessOrInput{Access: access},
		})
	} else {
		input, err := rawSpec(map[string]any{"type": "Dir/v1", "path": joinPath(".")})
		if err != nil {
			return constructorv1.Component{}, ComponentReport{}, err
		}
		sources = append(sources, constructorv1.Source{
			ElementMeta:   constructorv1.ElementMeta{ObjectMeta: constructorv1.ObjectMeta{Name: "source", Version: version}},
			Type:          "dir",
			AccessOrInput: constructorv1.AccessOrInput{Input: input},
		})
	}

	var resources []constructorv1.Resource
	seen := map[string]bool{}
	add := func(r constructorv1.Resource) bool {
		if seen[r.Name] {
			return false
		}
		seen[r.Name] = true
		resources = append(resources, r)
		return true
	}
	dirInput := func(path string, excludeGit bool) (*runtime.Raw, error) {
		spec := map[string]any{"type": "Dir/v1", "path": path}
		if excludeGit {
			spec["excludeFiles"] = []string{".git"}
		}
		return rawSpec(spec)
	}

	primaryTODOs, err := appendPrimaryResource(dec, add, dirInput, joinPath, chartDir(state, dec), slug, version)
	if err != nil {
		return constructorv1.Component{}, ComponentReport{}, err
	}
	todos = append(todos, primaryTODOs...)

	primaryImageRef := ""
	if dec.Kind == "ociImage" && len(resources) > 0 {
		if a := resources[0].Access; a != nil {
			var spec map[string]any
			if json.Unmarshal(a.Data, &spec) == nil {
				primaryImageRef, _ = spec["imageReference"].(string)
			}
		}
	}
	artNotes, artTODOs, err := appendCIArtifacts(ctx, state, dec, add, dirInput, joinPath, name, slug, version, primaryImageRef)
	if err != nil {
		return constructorv1.Component{}, ComponentReport{}, err
	}
	dec.Notes = append(dec.Notes, artNotes...)
	todos = append(todos, artTODOs...)

	labels := []constructorv1.Label{
		mustLabel("classification.ocm.software/maturity", dec.Maturity),
		mustLabel("classification.ocm.software/versioning-scheme", dec.VersioningScheme),
	}
	if state.Branch != "" {
		labels = append(labels, mustLabel("classification.ocm.software/source-branch", state.Branch))
	}
	if versionSource != "" {
		labels = append(labels, mustLabel("classification.ocm.software/version-source", versionSource))
	}
	if state.License != "" {
		labels = append(labels, mustLabel("classification.ocm.software/license", state.License))
	}

	component := constructorv1.Component{
		ComponentMeta: constructorv1.ComponentMeta{ObjectMeta: constructorv1.ObjectMeta{Name: name, Version: version, Labels: labels}},
		Provider:      constructorv1.Provider{Name: provider},
		Resources:     resources,
		Sources:       sources,
	}
	report := ComponentReport{
		Name:          name,
		Version:       version,
		Kind:          dec.Kind,
		VersionSource: versionSource,
		Provider:      provider,
		Method:        dec.Method,
		Notes:         dec.Notes,
		TODOs:         todos,
	}
	resourceKinds := make([]string, 0, len(resources))
	for _, r := range resources {
		resourceKinds = append(resourceKinds, r.Type+":"+r.Name)
	}
	slogctx.FromCtx(ctx).Debug("init: assembled component",
		"component", name,
		"kind", dec.Kind,
		"version", version,
		"version_source", versionSource,
		"resources", resourceKinds,
		"todos", len(todos),
	)
	return component, report, nil
}

// appendPrimaryResource adds the component's primary deliverable resource for
// its classified kind. It returns any TODOs (e.g. a guessed image reference).
func appendPrimaryResource(
	dec *Decision,
	add func(constructorv1.Resource) bool,
	dirInput func(string, bool) (*runtime.Raw, error),
	joinPath func(string) string,
	chartPath, slug, version string,
) (todos []string, err error) {
	switch dec.Kind {
	case "goModule":
		input, ierr := dirInput(joinPath("."), true)
		if ierr != nil {
			return nil, ierr
		}
		add(constructorv1.Resource{
			ElementMeta:   constructorv1.ElementMeta{ObjectMeta: constructorv1.ObjectMeta{Name: "module", Version: version}},
			Type:          "blob",
			Relation:      constructorv1.LocalRelation,
			SourceRefs:    []constructorv1.SourceRef{{IdentitySelector: map[string]string{"name": "source"}}},
			AccessOrInput: constructorv1.AccessOrInput{Input: input},
		})
	case "helmChart":
		input, ierr := rawSpec(map[string]any{"type": "Helm/v1", "path": joinPath(chartPath)})
		if ierr != nil {
			return nil, ierr
		}
		add(constructorv1.Resource{
			ElementMeta:   constructorv1.ElementMeta{ObjectMeta: constructorv1.ObjectMeta{Name: "chart", Version: version}},
			Type:          "helmChart",
			Relation:      constructorv1.LocalRelation,
			AccessOrInput: constructorv1.AccessOrInput{Input: input},
		})
	case "ociImage":
		// Prefer an image reference discovered from the repo (values.yaml,
		// Makefile); only fall back to a conventional GHCR guess when nothing was
		// found, and flag that guess loudly for the user to verify.
		imageRef, guessed := imageReference(dec, slug, version)
		if guessed {
			todos = append(todos, fmt.Sprintf("verify the image reference %q on resource \"image\" — it is a best-effort guess from the repository origin, not a discovered value", imageRef))
		}
		access, ierr := rawSpec(map[string]any{"type": "OCIImage/v1", "imageReference": imageRef})
		if ierr != nil {
			return nil, ierr
		}
		add(constructorv1.Resource{
			ElementMeta:   constructorv1.ElementMeta{ObjectMeta: constructorv1.ObjectMeta{Name: "image", Version: version}},
			Type:          "ociImage",
			Relation:      constructorv1.ExternalRelation,
			AccessOrInput: constructorv1.AccessOrInput{Access: access},
		})
	default: // blob, npmPackage, pythonPackage
		input, ierr := dirInput(joinPath("."), false)
		if ierr != nil {
			return nil, ierr
		}
		add(constructorv1.Resource{
			ElementMeta:   constructorv1.ElementMeta{ObjectMeta: constructorv1.ObjectMeta{Name: "artifact", Version: version}},
			Type:          "blob",
			Relation:      constructorv1.LocalRelation,
			AccessOrInput: constructorv1.AccessOrInput{Input: input},
		})
	}
	return todos, nil
}

// appendCIArtifacts adds the CI/release outputs discovered from goreleaser,
// GitHub Actions, and build-system files as extra resources via add (which
// reports whether it actually appended, so duplicates are not annotated).
// primaryImageRef is the reference already emitted as the primary deliverable,
// if any, so it is not added twice. It returns the notes and TODOs to merge
// into the report.
func appendCIArtifacts(
	ctx context.Context,
	state *RepoState,
	dec *Decision,
	add func(constructorv1.Resource) bool,
	dirInput func(string, bool) (*runtime.Raw, error),
	joinPath func(string) string,
	name, slug, version, primaryImageRef string,
) (notes, todos []string, err error) {
	discovered := scanner.DiscoverArtifacts(state.fileset, version)
	if len(discovered) == 0 {
		return nil, nil, nil
	}
	srcs := make([]string, 0, len(discovered))
	for _, a := range discovered {
		label := a.Source + "/" + a.Kind
		if a.Ref != "" {
			label += "=" + a.Ref
		}
		srcs = append(srcs, label)
	}
	slogctx.FromCtx(ctx).Debug("init: discovered CI/release artifacts", "component", name, "count", len(discovered), "artifacts", srcs)

	for _, art := range discovered {
		switch art.Kind {
		case "ociImage":
			ref, guessed := ciImageRef(art, slug, version)
			if ref == primaryImageRef {
				continue // already emitted as the primary deliverable
			}
			resName := art.Name
			if resName == "" || resName == "image" {
				resName = "image-" + shortRefName(ref)
			}
			access, aerr := rawSpec(map[string]any{"type": "OCIImage/v1", "imageReference": ref})
			if aerr != nil {
				return nil, nil, aerr
			}
			if add(constructorv1.Resource{
				ElementMeta:   constructorv1.ElementMeta{ObjectMeta: constructorv1.ObjectMeta{Name: resName, Version: version}},
				Type:          "ociImage",
				Relation:      constructorv1.ExternalRelation,
				AccessOrInput: constructorv1.AccessOrInput{Access: access},
			}) {
				if guessed {
					todos = append(todos, fmt.Sprintf("verify the image reference %q on resource %q — %s could not be fully resolved", ref, resName, art.Source))
				} else {
					notes = append(notes, art.Note+" — "+ref)
				}
			}
		case "executable":
			// A released binary is an external asset not present in the local
			// checkout; model it as an external blob and tell the user to point
			// the access at the real built artifact.
			var labels []constructorv1.Label
			if len(art.Platforms) > 0 {
				labels = append(labels, mustLabel("classification.ocm.software/platforms", art.Platforms))
			}
			input, ierr := dirInput(joinPath("."), false)
			if ierr != nil {
				return nil, nil, ierr
			}
			if add(constructorv1.Resource{
				ElementMeta:   constructorv1.ElementMeta{ObjectMeta: constructorv1.ObjectMeta{Name: art.Name, Version: version, Labels: labels}},
				Type:          "blob",
				Relation:      constructorv1.LocalRelation,
				AccessOrInput: constructorv1.AccessOrInput{Input: input},
			}) {
				todos = append(todos, fmt.Sprintf("resource %q is a released binary (%s); point its input at the built artifact rather than the source directory", art.Name, art.Source))
			}
		}
	}
	return notes, todos, nil
}

// ciImageRef resolves a discovered image artifact to a full reference, appending
// the version when the artifact carries no tag and falling back to a flagged
// GHCR guess when the reference could not be resolved at all (e.g. ko).
func ciImageRef(art scanner.Artifact, slug, version string) (ref string, guessed bool) {
	ref = art.Ref
	switch {
	case ref == "":
		return fmt.Sprintf("ghcr.io/%s:%s", slug, version), true
	case !strings.Contains(lastPathSeg(ref), ":"):
		return ref + ":" + version, art.Guessed
	default:
		return ref, art.Guessed
	}
}

// Classifier turns extracted evidence into a Decision. The default is the
// deterministic ClassifyRules; the Jev enhancer is injected optionally.
type Classifier func(ctx context.Context, state *RepoState) (*Decision, error)

// BuildConstructorTree classifies the repository at root with the given
// classifier and returns a component-constructor spec. When the root is
// classified as a monorepo, it fans out: one component per sub-deliverable (each
// at its own resolved version) plus a thin root aggregate whose
// componentReferences point at them. Otherwise it emits a single component.
//
// When offline is false, GitHub Releases are fetched (best-effort) via httpClient
// to refine version resolution; offline resolves versions from git tags only.
// httpClient is the OCM-configured client; when nil the http binding default is used.
func BuildConstructorTree(ctx context.Context, classify Classifier, root string, offline bool, httpClient *http.Client) (*constructorv1.ComponentConstructor, *Report, error) {
	rootState, err := extractState(ctx, root)
	if err != nil {
		return nil, nil, err
	}

	releases := &releaseData{Available: false}
	if !offline {
		releases = discoverReleases(ctx, rootState.OriginURL, httpClient)
	}

	rootDec, err := classify(ctx, rootState)
	if err != nil {
		return nil, nil, err
	}

	logger := slogctx.FromCtx(ctx)
	logger.Debug("init: classified repository",
		"method", rootDec.Method,
		"kind", rootDec.Kind,
		"provider", rootDec.Provider,
		"source_access", rootDec.SourceAccess,
		"maturity", rootDec.Maturity,
		"versioning_scheme", rootDec.VersioningScheme,
		"ships_container", rootDec.ShipsContainer,
		"ships_helm", rootDec.ShipsHelm,
		"monorepo", rootDec.IsMonorepo,
		"image_hints", rootDec.ImageHints,
	)

	report := &Report{}

	if !rootDec.IsMonorepo {
		res := resolveVersion("", rootState.versions, releases, rootState.HeadCommit)
		logger.Debug("init: resolved version", "component", "root", "version", res.Version, "source", res.Source)
		component, compReport, err := buildComponent(ctx, rootState, rootDec, res.Version, res.Source, "")
		if err != nil {
			return nil, nil, err
		}
		report.Components = append(report.Components, compReport)
		logger.Debug("init: discovered a single component", "component", compReport.Name, "kind", compReport.Kind, "version", compReport.Version)
		return &constructorv1.ComponentConstructor{Components: []constructorv1.Component{component}}, report, nil
	}

	report.Monorepo = true
	var subdirs []string
	if rootDec.comp != nil {
		subdirs = rootDec.comp.SubDirs
	}
	logger.Debug("init: discovered a monorepo", "sub_deliverables", len(subdirs), "dirs", subdirs)

	var components []constructorv1.Component
	var references []constructorv1.Reference
	for _, subdir := range subdirs {
		subRoot := filepath.Join(root, subdir)
		subState, err := extractState(ctx, subRoot)
		if err != nil {
			return nil, nil, fmt.Errorf("classifying sub-deliverable %q: %w", subdir, err)
		}
		// Sub-deliverables share the repo's git identity (origin/commit/branch/tags).
		subState.OriginURL = rootState.OriginURL
		subState.HeadCommit = rootState.HeadCommit
		subState.Branch = rootState.Branch
		subState.versions = rootState.versions

		subDec, err := classify(ctx, subState)
		if err != nil {
			return nil, nil, fmt.Errorf("classifying sub-deliverable %q: %w", subdir, err)
		}
		res := resolveVersion(subdir, rootState.versions, releases, rootState.HeadCommit)
		logger.Debug("init: resolved version", "component", subdir, "version", res.Version, "source", res.Source)

		// The sub-component name is the root name suffixed with the subdir so
		// each sub-deliverable is a distinct component.
		subState.RepoName = rootState.RepoName + "/" + subdir
		component, compReport, err := buildComponent(ctx, subState, subDec, res.Version, res.Source, subdir)
		if err != nil {
			return nil, nil, err
		}
		// Ensure a unique component name per subtree even when origin is shared.
		component.Name = componentName(rootState) + "/" + subdir
		compReport.Name = component.Name
		components = append(components, component)
		report.Components = append(report.Components, compReport)

		references = append(references, constructorv1.Reference{
			ElementMeta: constructorv1.ElementMeta{ObjectMeta: constructorv1.ObjectMeta{Name: strings.ReplaceAll(subdir, "/", "-"), Version: res.Version}},
			Component:   component.Name,
		})
	}

	// Root aggregate component: references + a source for the repo itself.
	rootRes := resolveVersion("", rootState.versions, releases, rootState.HeadCommit)
	rootName := componentName(rootState)
	var rootSources []constructorv1.Source
	if strings.HasPrefix(rootState.OriginURL, "http") && rootState.HeadCommit != "" {
		access, err := rawSpec(map[string]any{
			"type":    "GitHub/v1",
			"repoUrl": stripGitRE.ReplaceAllString(rootState.OriginURL, ""),
			"commit":  rootState.HeadCommit,
		})
		if err != nil {
			return nil, nil, err
		}
		rootSources = append(rootSources, constructorv1.Source{
			ElementMeta:   constructorv1.ElementMeta{ObjectMeta: constructorv1.ObjectMeta{Name: "source", Version: rootRes.Version}},
			Type:          "git",
			AccessOrInput: constructorv1.AccessOrInput{Access: access},
		})
	} else {
		input, err := rawSpec(map[string]any{"type": "Dir/v1", "path": "."})
		if err != nil {
			return nil, nil, err
		}
		rootSources = append(rootSources, constructorv1.Source{
			ElementMeta:   constructorv1.ElementMeta{ObjectMeta: constructorv1.ObjectMeta{Name: "source", Version: rootRes.Version}},
			Type:          "dir",
			AccessOrInput: constructorv1.AccessOrInput{Input: input},
		})
	}
	rootProvider := rootDec.Provider
	if rootProvider == "" {
		rootProvider = "ocm.software"
	}
	rootComponent := constructorv1.Component{
		ComponentMeta: constructorv1.ComponentMeta{ObjectMeta: constructorv1.ObjectMeta{
			Name:    rootName,
			Version: rootRes.Version,
			Labels: []constructorv1.Label{
				mustLabel("classification.ocm.software/maturity", rootDec.Maturity),
				mustLabel("classification.ocm.software/versioning-scheme", rootDec.VersioningScheme),
				mustLabel("classification.ocm.software/monorepo", true),
			},
		}},
		Provider:   constructorv1.Provider{Name: rootProvider},
		Resources:  []constructorv1.Resource{},
		Sources:    rootSources,
		References: references,
	}
	report.Components = append(report.Components, ComponentReport{
		Name:          rootName,
		Version:       rootRes.Version,
		Kind:          "aggregate",
		VersionSource: rootRes.Source,
		Provider:      rootProvider,
		Method:        rootDec.Method,
		Notes:         []string{fmt.Sprintf("monorepo root aggregating %d sub-component(s)", len(references))},
	})

	// Root aggregate first, then sub-components.
	all := append([]constructorv1.Component{rootComponent}, components...)
	return &constructorv1.ComponentConstructor{Components: all}, report, nil
}
