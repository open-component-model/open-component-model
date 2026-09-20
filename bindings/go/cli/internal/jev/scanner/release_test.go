package scanner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveTemplate pins that known vars resolve and unknown constructs mark
// the result unresolved.
func TestResolveTemplate(t *testing.T) {
	r := require.New(t)

	out, ok := resolveTemplate("ghcr.io/acme/tool:{{.Version}}", "1.2.0")
	r.Equal("ghcr.io/acme/tool:1.2.0", out)
	r.True(ok)

	out, ok = resolveTemplate("ghcr.io/acme/tool:{{ .Tag }}", "1.2.0")
	r.Equal("ghcr.io/acme/tool:1.2.0", out)
	r.True(ok)

	_, ok = resolveTemplate("ghcr.io/acme/tool:{{.Env.CUSTOM}}", "1.2.0")
	r.False(ok, "unknown template var is not resolved")

	_, ok = resolveTemplate(`ghcr.io/acme/tool:{{ trimprefix .Tag "v" }}`, "1.2.0")
	r.False(ok, "a template function leaves the ref unresolved")
}

// TestGoreleaserArtifacts pins image + binary-matrix extraction with template
// resolution.
func TestGoreleaserArtifacts(t *testing.T) {
	r := require.New(t)

	cfg := `
builds:
  - id: tool
    binary: tool
    goos: [linux, darwin]
    goarch: [amd64, arm64]
dockers:
  - image_templates:
      - "ghcr.io/acme/tool:{{ .Version }}"
      - "ghcr.io/acme/tool:latest"
`
	arts := goreleaserArtifacts(fs(map[string]string{".goreleaser.yaml": cfg}), "1.2.0")

	var images, bins []Artifact
	for _, a := range arts {
		switch a.Kind {
		case "ociImage":
			images = append(images, a)
		case "executable":
			bins = append(bins, a)
		}
	}
	r.Len(images, 2)
	r.Equal("ghcr.io/acme/tool:1.2.0", images[0].Ref)
	r.False(images[0].Guessed)
	r.Equal("image", images[0].Name)

	r.Len(bins, 1)
	r.Equal("binary-tool", bins[0].Name)
	r.Equal([]string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"}, bins[0].Platforms)
}

// TestGithubActionsArtifacts pins build-push-action image extraction, including
// bare tags combined with metadata-action images, and gh-release binaries.
func TestGithubActionsArtifacts(t *testing.T) {
	r := require.New(t)

	wf := `
jobs:
  release:
    steps:
      - uses: docker/metadata-action@v5
        with:
          images: ghcr.io/acme/tool
      - uses: docker/build-push-action@v6
        with:
          push: true
          tags: |
            ghcr.io/acme/tool:{{ .Version }}
      - uses: softprops/action-gh-release@v2
`
	arts := githubActionsArtifacts(fs(map[string]string{".github/workflows/release.yml": wf}), "1.2.0")

	var images, bins int
	for _, a := range arts {
		switch a.Kind {
		case "ociImage":
			images++
			r.Equal("ghcr.io/acme/tool:1.2.0", a.Ref)
			r.False(a.Guessed)
		case "executable":
			bins++
			r.True(a.Guessed, "release binary asset names are not statically known")
		}
	}
	r.Equal(1, images)
	r.Equal(1, bins)
}

// TestGithubActionsSkipsNonPush pins that a push:false build is not emitted.
func TestGithubActionsSkipsNonPush(t *testing.T) {
	r := require.New(t)
	wf := `
jobs:
  ci:
    steps:
      - uses: docker/build-push-action@v6
        with:
          push: false
          tags: ghcr.io/acme/tool:test
`
	arts := githubActionsArtifacts(fs(map[string]string{".github/workflows/ci.yml": wf}), "1.2.0")
	r.Empty(arts, "a non-pushing build produces no published image")
}

// TestBuildSystemArtifacts pins Makefile push + Taskfile var + ko detection.
func TestBuildSystemArtifacts(t *testing.T) {
	r := require.New(t)

	arts := buildSystemArtifacts(fs(map[string]string{
		"Makefile": "IMG ?= ghcr.io/acme/tool\nrelease:\n\tdocker push ghcr.io/acme/other\n\tko build ./cmd\n",
	}), "1.2.0")

	refs := map[string]bool{}
	ko := false
	for _, a := range arts {
		if a.Ref != "" {
			refs[a.Ref] = true
		}
		if a.Ref == "" && a.Guessed {
			ko = true
		}
	}
	r.True(refs["ghcr.io/acme/tool"], "IMG image detected")
	r.True(refs["ghcr.io/acme/other"], "docker push image detected")
	r.True(ko, "ko usage detected as an unresolved image")
}

// TestDiscoverArtifactsDedup pins that the same image named by several sources
// appears once.
func TestDiscoverArtifactsDedup(t *testing.T) {
	r := require.New(t)

	files := map[string]string{
		".goreleaser.yaml": "dockers:\n  - image_templates: [\"ghcr.io/acme/tool:{{.Version}}\"]\n",
		"Makefile":         "IMG ?= ghcr.io/acme/tool:1.2.0\n",
	}
	arts := DiscoverArtifacts(fs(files), "1.2.0")
	count := 0
	for _, a := range arts {
		if a.Ref == "ghcr.io/acme/tool:1.2.0" {
			count++
		}
	}
	r.Equal(1, count, "the same image reference is emitted once across sources")
}
