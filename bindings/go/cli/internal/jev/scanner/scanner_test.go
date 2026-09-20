package scanner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// fs is a shorthand for an in-memory FileSet rooted at a dummy path.
func fs(contents map[string]string) *FileSet { return NewFileSet("/repo", contents) }

// TestComposePriority pins that the dominant kind is chosen by priority: a chart
// outranks a container image even when the image is at the root.
func TestComposePriority(t *testing.T) {
	r := require.New(t)

	// ocm-controller shape: root Dockerfile + go main, chart in deploy/.
	c := map[string]string{
		"go.mod":            "module x\n",
		"main.go":           "package main\nfunc main(){}\n",
		"Dockerfile":        "FROM scratch\n",
		"deploy/Chart.yaml": "name: ocm-controller\n",
	}
	comp := Compose(fs(c), DefaultRegistry().Scan(fs(c)))
	r.Equal("helmChart", comp.RootKind, "chart outranks a root Dockerfile")
	r.Equal("deploy", comp.Dominant.Dir)
	r.False(comp.IsMonorepo, "root Dockerfile/go make the repo a single component")
	r.True(comp.ShipsContainer)
	r.True(comp.ShipsHelm)
}

// TestHelmScanner pins chart detection, the dash-tag namespace from name:, and
// the dotted-host image hint from a sibling values.yaml.
func TestHelmScanner(t *testing.T) {
	r := require.New(t)

	f := fs(map[string]string{
		"deploy/Chart.yaml":  "apiVersion: v2\nname: ocm-controller\nappVersion: \"1.2.3\"\n",
		"deploy/values.yaml": "image:\n  repository: ghcr.io/acme/svc\n  tag: latest\nsub:\n  repository: bareregistry\n",
	})
	cands := helmScanner{}.Scan(f)
	r.Len(cands, 1)
	c := cands[0]
	r.Equal("deploy", c.Dir)
	r.Equal("helmChart", c.Kind)
	r.Equal(PriorityHelmChart, c.Priority)
	r.Equal("ocm-controller", c.TagPrefix)
	r.Equal([]string{"ghcr.io/acme/svc"}, c.ImageHints, "dotted-host repo kept; bare subchart repo ignored")
}

// TestDockerScanner pins the Makefile IMG hint extraction.
func TestDockerScanner(t *testing.T) {
	r := require.New(t)

	f := fs(map[string]string{
		"Dockerfile": "FROM scratch\n",
		"Makefile":   "IMG ?= ghcr.io/open-component-model/ocm-controller\nOTHER := $(FOO)\n",
	})
	cands := dockerScanner{}.Scan(f)
	r.Len(cands, 1)
	r.Equal("ociImage", cands[0].Kind)
	r.True(cands[0].Runnable)
	r.Equal([]string{"ghcr.io/open-component-model/ocm-controller"}, cands[0].ImageHints)
}

// TestGoScanner pins the runnable-vs-library distinction from a main entrypoint.
func TestGoScanner(t *testing.T) {
	r := require.New(t)

	runnable := goScanner{}.Scan(fs(map[string]string{
		"go.mod":          "module x\n",
		"cmd/app/main.go": "package main\nfunc main(){}\n",
		"internal/lib.go": "package internal\n",
	}))
	r.Len(runnable, 1)
	r.Equal("blob", runnable[0].Kind)
	r.True(runnable[0].Runnable)
	r.Equal(PriorityBinary, runnable[0].Priority)

	lib := goScanner{}.Scan(fs(map[string]string{
		"go.mod":     "module x\n",
		"pkg/lib.go": "package pkg\nfunc F(){}\n",
	}))
	r.Len(lib, 1)
	r.Equal("goModule", lib[0].Kind)
	r.False(lib[0].Runnable)
	r.Equal(PrioritySourceLib, lib[0].Priority)
}

// TestComposeMonorepo pins the conservative monorepo decision and its go.work
// suppression.
func TestComposeMonorepo(t *testing.T) {
	r := require.New(t)

	charts := map[string]string{
		"charts/a/Chart.yaml": "name: a\n",
		"charts/b/Chart.yaml": "name: b\n",
	}
	comp := Compose(fs(charts), DefaultRegistry().Scan(fs(charts)))
	r.True(comp.IsMonorepo, "sibling charts with no root deliverable fan out")
	r.Equal([]string{"charts/a", "charts/b"}, comp.SubDirs)

	withWork := map[string]string{
		"charts/a/Chart.yaml": "name: a\n",
		"charts/b/Chart.yaml": "name: b\n",
		"go.work":             "go 1.24\n",
	}
	comp = Compose(fs(withWork), DefaultRegistry().Scan(fs(withWork)))
	r.False(comp.IsMonorepo, "a go.work suppresses monorepo fan-out")
}

// TestComposeScaffoldingNotMonorepo pins that a single component keeping build
// scaffolding in subdirs stays a single component (the ocm-controller case).
func TestComposeScaffoldingNotMonorepo(t *testing.T) {
	r := require.New(t)

	c := map[string]string{
		"go.mod":             "module x\n",
		"main.go":            "package main\nfunc main(){}\n",
		"Dockerfile":         "FROM scratch\n",
		"deploy/Chart.yaml":  "name: ctl\n",
		"component/Makefile": "all:\n",
	}
	comp := Compose(fs(c), DefaultRegistry().Scan(fs(c)))
	r.False(comp.IsMonorepo)
	r.Equal("helmChart", comp.RootKind)
}

// TestJavaScanner pins Maven/Gradle explicit notes (kind stays blob).
func TestJavaScanner(t *testing.T) {
	r := require.New(t)

	cands := javaScanner{}.Scan(fs(map[string]string{
		"svc/pom.xml":      "<project/>\n",
		"lib/build.gradle": "plugins {}\n",
	}))
	r.Len(cands, 2)
	for _, c := range cands {
		r.Equal("blob", c.Kind)
		r.Equal(PriorityLangPkg, c.Priority)
	}
}
