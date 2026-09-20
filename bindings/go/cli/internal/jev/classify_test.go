package jev

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/internal/jev/scanner"
)

// TestSemverOrdering pins the comparator: release > prerelease, numeric ordering.
func TestSemverOrdering(t *testing.T) {
	r := require.New(t)
	r.True(lessSemver(semverKey("9.0.0"), semverKey("10.0.0")), "9.0.0 < 10.0.0 (numeric, not lexical)")
	r.True(lessSemver(semverKey("1.2.3-rc.1"), semverKey("1.2.3")), "prerelease < release")
	r.True(lessSemver(semverKey("1.0.0-rc.2"), semverKey("1.0.0-rc.10")), "rc.2 < rc.10")
	r.True(lessSemver(semverKey("not-a-version"), semverKey("0.0.1")), "invalid < valid")
}

// TestSplitTag pins tag parsing across the three real-world shapes: bare,
// slash-namespaced, and Helm-style dash-delimited (grafana-10.4.0).
func TestSplitTag(t *testing.T) {
	r := require.New(t)

	cases := []struct{ tag, wantPfx, wantVer string }{
		{"v1.4.2", "", "1.4.2"},
		{"services/api/v0.21.1", "services/api", "0.21.1"},
		{"deploy/chart/2.1.0", "deploy/chart", "2.1.0"},
		{"grafana-10.4.0", "grafana", "10.4.0"},
		{"agent-operator-0.10.0", "agent-operator", "0.10.0"},
		{"loki-distributed-0.80.6", "loki-distributed", "0.80.6"},
	}
	for _, c := range cases {
		pfx, ver, ok := splitTag(c.tag)
		r.True(ok, "tag %q should parse", c.tag)
		r.Equal(c.wantPfx, pfx, "prefix for %q", c.tag)
		r.Equal(c.wantVer, ver, "version for %q", c.tag)
	}

	_, _, ok := splitTag("nightly")
	r.False(ok, "non-semver tag must be rejected")
}

// TestResolveVersionPrecedence pins resolution precedence and the dash-namespace
// match a Helm-chart monorepo relies on.
func TestResolveVersionPrecedence(t *testing.T) {
	r := require.New(t)

	vd := &versionData{Namespaces: map[string]namespaceVersions{
		"":                 {Latest: "3.3.1", LatestStable: "3.3.1"},
		"services/api":     {Latest: "1.4.0", LatestStable: "1.4.0"},
		"loki-distributed": {Latest: "0.80.6", LatestStable: "0.80.6"},
	}}

	res := resolveVersion("services/api", vd, &releaseData{Available: false}, "deadbeefcafe0000")
	r.Equal("1.4.0", res.Version)
	r.Equal("git-tag", res.Source)

	// A chart subdir resolves against the dash-delimited tag namespace by its
	// directory basename (charts/loki-distributed -> namespace loki-distributed).
	res = resolveVersion("charts/loki-distributed", vd, &releaseData{Available: false}, "deadbeefcafe0000")
	r.Equal("0.80.6", res.Version)
	r.Equal("git-tag", res.Source)

	res = resolveVersion("cmd/tool", vd, &releaseData{Available: false}, "deadbeefcafe0000")
	r.Equal("3.3.1", res.Version)
	r.Equal("git-tag-root-fallback", res.Source)

	releases := &releaseData{Available: true, Namespaces: map[string][]releaseRecord{
		"services/api": {{Version: "1.5.0", Tag: "services/api/v1.5.0"}},
	}}
	res = resolveVersion("services/api", vd, releases, "deadbeefcafe0000")
	r.Equal("1.5.0", res.Version)
	r.Equal("github-release", res.Source)

	res = resolveVersion("x", &versionData{Namespaces: map[string]namespaceVersions{}}, &releaseData{Available: false}, "deadbeefcafe0000")
	r.Equal("0.0.0+deadbeefcafe", res.Version)
	r.Equal("pseudo", res.Source)
}

// stateWith builds a RepoState whose scanner FileSet is populated from the given
// repo-relative file contents, plus the given git identity fields.
func stateWith(origin string, files map[string]string) *RepoState {
	return &RepoState{OriginURL: origin, fileset: scanner.NewFileSet("/repo", files)}
}

// TestClassifyRulesKind pins the deterministic kind priority against the shapes
// seen in real repos: chart > image(+entrypoint) > runnable Go > library > blob.
func TestClassifyRulesKind(t *testing.T) {
	r := require.New(t)

	// ocm-controller shape: root go.mod + Dockerfile + deploy/Chart.yaml -> chart wins.
	r.Equal("helmChart", ClassifyRules(stateWith("", map[string]string{
		"go.mod":            "module x\n",
		"main.go":           "package main\nfunc main(){}\n",
		"Dockerfile":        "FROM scratch\n",
		"deploy/Chart.yaml": "name: ctl\n",
	})).Kind)

	// prometheus shape: root go.mod + Dockerfile + cmd/ -> ociImage.
	r.Equal("ociImage", ClassifyRules(stateWith("", map[string]string{
		"go.mod":          "module x\n",
		"cmd/app/main.go": "package main\nfunc main(){}\n",
		"Dockerfile":      "FROM scratch\n",
	})).Kind)

	// cobra shape: go.mod only, but has an example main -> runnable blob.
	r.Equal("blob", ClassifyRules(stateWith("", map[string]string{
		"go.mod":  "module x\n",
		"main.go": "package main\nfunc main(){}\n",
	})).Kind)

	// pure library: go.mod, no main -> goModule (source library).
	lib := ClassifyRules(stateWith("", map[string]string{
		"go.mod":     "module x\n",
		"pkg/lib.go": "package pkg\n",
	}))
	r.Equal("goModule", lib.Kind)
	r.Contains(lib.Notes[0], "library")

	// nothing recognised -> blob.
	r.Equal("blob", ClassifyRules(stateWith("", map[string]string{"README.md": "hi\n"})).Kind)
}

// TestClassifyRulesMonorepo pins the conservative monorepo decision: a single
// component with build scaffolding stays single; sibling charts with no root
// deliverable fan out. These are the exact real-repo false/true-positive cases.
func TestClassifyRulesMonorepo(t *testing.T) {
	r := require.New(t)

	// ocm-controller: dominant root go.mod+Dockerfile + component/, deploy/
	// scaffolding -> NOT a monorepo.
	r.False(ClassifyRules(stateWith("", map[string]string{
		"go.mod":             "module x\n",
		"main.go":            "package main\nfunc main(){}\n",
		"Dockerfile":         "FROM scratch\n",
		"deploy/Chart.yaml":  "name: ctl\n",
		"component/Makefile": "all:\n",
	})).IsMonorepo, "controller with scaffolding is one component")

	// prometheus: root go.mod+Dockerfile+cmd + internal/tools, compliance
	// submodules + a go.work -> NOT a monorepo.
	r.False(ClassifyRules(stateWith("", map[string]string{
		"go.mod":                "module x\n",
		"cmd/app/main.go":       "package main\nfunc main(){}\n",
		"Dockerfile":            "FROM scratch\n",
		"internal/tools/go.mod": "module t\n",
		"compliance/go.mod":     "module c\n",
		"go.work":               "go 1.24\n",
	})).IsMonorepo, "prometheus with tooling submodules + go.work is one component")

	// grafana-helm: no root deliverable, many sibling charts -> monorepo.
	r.True(ClassifyRules(stateWith("", map[string]string{
		"charts/grafana/Chart.yaml": "name: grafana\n",
		"charts/loki/Chart.yaml":    "name: loki\n",
		"charts/tempo/Chart.yaml":   "name: tempo\n",
	})).IsMonorepo, "sibling charts with no root deliverable is a monorepo")
}

// TestClassifyRulesDeterministic pins that the default classifier derives its
// core facts from evidence with no model: provider from origin, github source,
// maturity from tags, and confidence 1.0.
func TestClassifyRulesDeterministic(t *testing.T) {
	r := require.New(t)

	state := &RepoState{
		RepoName:   "ocm-controller",
		OriginURL:  "https://github.com/open-component-model/ocm-controller.git",
		HeadCommit: "8b0631258c0f53b3478c0b69dd8c6852f810bdbc",
		Branch:     "main",
		fileset:    scanner.NewFileSet("/repo", map[string]string{"deploy/Chart.yaml": "name: ocm-controller\n"}),
		versions:   &versionData{Namespaces: map[string]namespaceVersions{"": {Latest: "0.33.0", LatestStable: "0.33.0", Count: 20}}},
	}
	dec := ClassifyRules(state)

	r.Equal("helmChart", dec.Kind)
	r.Equal("rules", dec.Method)
	r.Equal("github", dec.SourceAccess)
	r.Equal("github.com/open-component-model", dec.Provider)
	r.Equal("active", dec.Maturity, "v0.x is pre-1.0 -> active, not stable")
	r.Equal(1.0, dec.MinConfidence)
	r.False(dec.IsMonorepo)
}

// TestLicenseID pins the conservative SPDX detection: recognised headers map to
// an id, unrecognised text yields "".
func TestLicenseID(t *testing.T) {
	r := require.New(t)
	r.Equal("Apache-2.0", licenseID("                                 Apache License\n                           Version 2.0, January 2004\n"))
	r.Equal("MIT", licenseID("MIT License\n\nCopyright (c) 2024\n"))
	r.Equal("BSD-3-Clause", licenseID("Redistributions of source code must retain the above copyright notice\n"))
	r.Equal("", licenseID("some random text with no license header"))
}
