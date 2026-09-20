package jev

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/cli/internal/jev/scanner"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
)

// TestMustLabelEncodesValidJSON pins that a scalar label value is encoded as a
// JSON scalar (quoted string / bare bool), not a bare YAML scalar. A regression
// here makes the whole constructor unmarshallable.
func TestMustLabelEncodesValidJSON(t *testing.T) {
	r := require.New(t)

	l := mustLabel("classification.ocm.software/maturity", "stable")
	r.JSONEq(`"stable"`, string(l.Value))

	b := mustLabel("classification.ocm.software/monorepo", true)
	r.JSONEq(`true`, string(b.Value))

	cc := &constructorv1.ComponentConstructor{Components: []constructorv1.Component{{
		ComponentMeta: constructorv1.ComponentMeta{ObjectMeta: constructorv1.ObjectMeta{
			Name: "ocm.software/test", Version: "1.0.0", Labels: []constructorv1.Label{l, b},
		}},
		Provider:  constructorv1.Provider{Name: "vendor"},
		Resources: []constructorv1.Resource{},
		Sources:   []constructorv1.Source{},
	}}}
	out, err := yaml.Marshal(cc)
	r.NoError(err, "constructor with labels must marshal")

	var back constructorv1.ComponentConstructor
	r.NoError(yaml.Unmarshal(out, &back), "marshaled constructor must round-trip")
	r.Equal("ocm.software/test", back.Components[0].Name)
}

// TestBuildComponentHelmChart pins the assembled shape for a helm-chart repo: a
// helmChart resource with a Helm/v1 input at the detected chart dir, a GitHub/v1
// source at the full commit, and the classification labels. No TODOs.
func TestBuildComponentHelmChart(t *testing.T) {
	r := require.New(t)

	state := &RepoState{
		RepoName:   "hello-chart",
		OriginURL:  "https://github.com/acme/hello-chart.git",
		Branch:     "main",
		HeadCommit: "12fa7cbafa4797a42aa5b2902d628aeb9da1bbb3",
		fileset:    scanner.NewFileSet("/repo", map[string]string{"chart/Chart.yaml": "name: hello\n"}),
	}
	dec := ClassifyRules(state)
	r.Equal("helmChart", dec.Kind)

	comp, report, err := buildComponent(t.Context(), state, dec, "1.4.2", "git-tag", "")
	r.NoError(err)

	r.Equal("github.com/acme/hello-chart", comp.Name)
	r.Equal("github.com/acme", comp.Provider.Name, "provider derived from origin org")
	r.Empty(report.TODOs, "a chart has no guessed fields")

	r.Len(comp.Resources, 1)
	res := comp.Resources[0]
	r.Equal("helmChart", res.Type)
	var input map[string]any
	r.NoError(json.Unmarshal(res.Input.Data, &input))
	r.Equal("Helm/v1", input["type"])
	r.Equal("chart", input["path"], "chart path derived from the detected Chart.yaml location")

	r.Len(comp.Sources, 1)
	var access map[string]any
	r.NoError(json.Unmarshal(comp.Sources[0].Access.Data, &access))
	r.Equal("GitHub/v1", access["type"])
	r.Equal("https://github.com/acme/hello-chart", access["repoUrl"], ".git suffix stripped")
	r.Equal(state.HeadCommit, access["commit"], "full 40-char commit required by GitHub/v1")
}

// TestBuildComponentOCIImageFlagsGuess pins that a guessed image reference is
// reported as a TODO the user must verify — never emitted silently.
func TestBuildComponentOCIImageFlagsGuess(t *testing.T) {
	r := require.New(t)

	state := &RepoState{
		RepoName:   "prometheus",
		OriginURL:  "https://github.com/prometheus/prometheus.git",
		HeadCommit: "abc123def456abc123def456abc123def456abcd",
		fileset: scanner.NewFileSet("/repo", map[string]string{
			"go.mod":          "module x\n",
			"cmd/app/main.go": "package main\nfunc main(){}\n",
			"Dockerfile":      "FROM scratch\n",
		}),
	}
	dec := ClassifyRules(state)
	r.Equal("ociImage", dec.Kind)

	comp, report, err := buildComponent(t.Context(), state, dec, "3.14.0", "git-tag", "")
	r.NoError(err)

	r.Len(comp.Resources, 1)
	r.Equal("ociImage", comp.Resources[0].Type)
	r.NotEmpty(report.TODOs, "a guessed image reference must be surfaced as a TODO")
	r.Contains(report.TODOs[0], "image reference", "TODO names the field to verify")
}

// TestBuildComponentSubtreePathPrefix pins that a sub-deliverable's input paths
// are rewritten relative to the repo root so one --working-directory builds them.
func TestBuildComponentSubtreePathPrefix(t *testing.T) {
	r := require.New(t)

	state := &RepoState{
		RepoName:   "platform",
		OriginURL:  "https://github.com/acme/platform.git",
		HeadCommit: "ca6f8794a41c3ad15c86d9dd12e2fb99c90883e1",
		fileset:    scanner.NewFileSet("/repo", map[string]string{"go.mod": "module x\n", "lib.go": "package p\n"}),
	}
	dec := ClassifyRules(state)
	r.Equal("goModule", dec.Kind, "go.mod without a main is a library")

	comp, _, err := buildComponent(t.Context(), state, dec, "0.9.4", "git-tag", "libs/common")
	r.NoError(err)
	r.Len(comp.Resources, 1)
	var input map[string]any
	r.NoError(json.Unmarshal(comp.Resources[0].Input.Data, &input))
	r.Equal("libs/common", input["path"], "input path prefixed with the subtree dir")
}

// TestBuildConstructorTreeSingleComponent pins the end-to-end deterministic path
// for a single-component repo: one component, buildable-shaped, method "rules".
func TestBuildConstructorTreeSingleComponent(t *testing.T) {
	r := require.New(t)

	// A repo with no monorepo signal classified purely by rules (offline, nil client).
	state := &RepoState{
		RepoName:   "hello-chart",
		OriginURL:  "https://github.com/acme/hello-chart.git",
		HeadCommit: "12fa7cbafa4797a42aa5b2902d628aeb9da1bbb3",
		fileset:    scanner.NewFileSet("/repo", map[string]string{"Chart.yaml": "name: hello\n"}),
	}
	dec := ClassifyRules(state)
	comp, report, err := buildComponent(t.Context(), state, dec, "1.0.0", "git-tag", "")
	r.NoError(err)
	r.Equal("rules", report.Method)
	r.Equal("helmChart", comp.Resources[0].Type)
}

// TestImageReferenceUsesHint pins that a discovered image hint beats the GHCR
// guess and is not flagged, while an absent hint falls back to a flagged guess.
func TestImageReferenceUsesHint(t *testing.T) {
	r := require.New(t)

	ref, guessed := imageReference(&Decision{ImageHints: []string{"ghcr.io/acme/svc"}}, "acme/svc", "1.2.0")
	r.Equal("ghcr.io/acme/svc:1.2.0", ref)
	r.False(guessed, "a discovered hint is not a guess")

	ref, guessed = imageReference(&Decision{ImageHints: []string{"ghcr.io/acme/svc:pinned"}}, "acme/svc", "1.2.0")
	r.Equal("ghcr.io/acme/svc:pinned", ref, "a hint that already carries a tag is used verbatim")
	r.False(guessed)

	ref, guessed = imageReference(&Decision{}, "acme/svc", "1.2.0")
	r.Equal("ghcr.io/acme/svc:1.2.0", ref)
	r.True(guessed, "no hint -> a flagged GHCR guess")
}

// TestBuildComponentUsesDiscoveredImage pins that a Makefile-discovered image
// reference is emitted without a verify-the-image TODO.
func TestBuildComponentUsesDiscoveredImage(t *testing.T) {
	r := require.New(t)

	state := &RepoState{
		RepoName:   "tool",
		OriginURL:  "https://github.com/acme/tool.git",
		HeadCommit: "aaaabbbbccccddddeeeeffff0000111122223333",
		fileset: scanner.NewFileSet("/repo", map[string]string{
			"Dockerfile":      "FROM scratch\n",
			"Makefile":        "IMG ?= ghcr.io/acme/tool\n",
			"cmd/app/main.go": "package main\nfunc main(){}\n",
			"go.mod":          "module x\n",
		}),
	}
	dec := ClassifyRules(state)
	r.Equal("ociImage", dec.Kind)

	comp, report, err := buildComponent(t.Context(), state, dec, "1.2.0", "git-tag", "")
	r.NoError(err)
	var access map[string]any
	r.NoError(json.Unmarshal(comp.Resources[0].Access.Data, &access))
	r.Equal("ghcr.io/acme/tool:1.2.0", access["imageReference"], "discovered Makefile image used")
	for _, todo := range report.TODOs {
		r.NotContains(todo, "image reference", "a discovered image is not flagged as a guess")
	}
}

// TestLicenseLabelEmitted pins that a detected license is added as a component
// label.
func TestLicenseLabelEmitted(t *testing.T) {
	r := require.New(t)

	state := &RepoState{
		RepoName:   "hello-chart",
		OriginURL:  "https://github.com/acme/hello-chart.git",
		HeadCommit: "12fa7cbafa4797a42aa5b2902d628aeb9da1bbb3",
		License:    "Apache-2.0",
		fileset:    scanner.NewFileSet("/repo", map[string]string{"Chart.yaml": "name: hello\n"}),
	}
	dec := ClassifyRules(state)
	comp, _, err := buildComponent(t.Context(), state, dec, "1.0.0", "git-tag", "")
	r.NoError(err)

	var found bool
	for _, l := range comp.Labels {
		if l.Name == "classification.ocm.software/license" {
			found = true
			r.JSONEq(`"Apache-2.0"`, string(l.Value))
		}
	}
	r.True(found, "a detected license must be emitted as a label")
}

// TestBuildComponentMultiArtifact pins that a component ships its primary
// deliverable PLUS the CI/release outputs discovered from goreleaser: an image
// resource and a released-binary blob, beyond the go-module source.
func TestBuildComponentMultiArtifact(t *testing.T) {
	r := require.New(t)

	goreleaser := `
builds:
  - binary: tool
    goos: [linux]
    goarch: [amd64, arm64]
dockers:
  - image_templates: ["ghcr.io/acme/tool:{{ .Version }}"]
`
	state := &RepoState{
		RepoName:   "tool",
		OriginURL:  "https://github.com/acme/tool.git",
		HeadCommit: "aaaabbbbccccddddeeeeffff0000111122223333",
		fileset: scanner.NewFileSet("/repo", map[string]string{
			"go.mod":           "module x\n",
			"pkg/lib.go":       "package pkg\n",
			".goreleaser.yaml": goreleaser,
		}),
	}
	dec := ClassifyRules(state)
	r.Equal("goModule", dec.Kind, "primary kind is the library module")

	comp, report, err := buildComponent(t.Context(), state, dec, "1.2.0", "git-tag", "")
	r.NoError(err)

	byType := map[string]int{}
	var imageRef string
	for _, res := range comp.Resources {
		byType[res.Type]++
		if res.Type == "ociImage" {
			var access map[string]any
			r.NoError(json.Unmarshal(res.Access.Data, &access))
			imageRef, _ = access["imageReference"].(string)
		}
	}
	r.Equal(1, byType["ociImage"], "the goreleaser image is added as a resource")
	r.Equal("ghcr.io/acme/tool:1.2.0", imageRef, "resolved from the goreleaser template")
	r.GreaterOrEqual(byType["blob"], 2, "the module blob plus the released binary blob")

	var binaryTODO bool
	for _, todo := range report.TODOs {
		if strings.Contains(todo, "released binary") {
			binaryTODO = true
		}
	}
	r.True(binaryTODO, "a released binary must be flagged to point at the built artifact")
}
