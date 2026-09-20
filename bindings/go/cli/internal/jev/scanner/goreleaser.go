package scanner

import (
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

const goreleaserReadCap = 32 << 10 // 32 KiB

// goreleaserConfig is the subset of a .goreleaser.yaml we read: the binary build
// matrix and the container image definitions.
type goreleaserConfig struct {
	Builds []struct {
		ID     string   `json:"id"`
		Binary string   `json:"binary"`
		Goos   []string `json:"goos"`
		Goarch []string `json:"goarch"`
	} `json:"builds"`
	Dockers []struct {
		ImageTemplates []string `json:"image_templates"`
	} `json:"dockers"`
	DockerManifests []struct {
		NameTemplate string `json:"name_template"`
	} `json:"docker_manifests"`
}

// goreleaserFiles are the recognised goreleaser config filenames, most specific
// first.
var goreleaserFiles = []string{".goreleaser.yaml", ".goreleaser.yml"}

// goreleaserArtifacts parses a root .goreleaser.yaml and returns the release
// outputs it declares: container images (with resolved tags where possible) and
// released binaries (with their platform matrix). version resolves template
// variables. Returns nil when there is no goreleaser config.
func goreleaserArtifacts(fs *FileSet, version string) []Artifact {
	var raw []byte
	for _, f := range goreleaserFiles {
		if b := fs.Read(f, goreleaserReadCap); b != nil {
			raw = b
			break
		}
	}
	if raw == nil {
		return nil
	}
	var cfg goreleaserConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil // malformed config: best-effort, no artifacts
	}

	var arts []Artifact

	// Images: image_templates on dockers, plus docker_manifests name_templates.
	var imageTmpls []string
	for _, d := range cfg.Dockers {
		imageTmpls = append(imageTmpls, d.ImageTemplates...)
	}
	for _, m := range cfg.DockerManifests {
		if m.NameTemplate != "" {
			imageTmpls = append(imageTmpls, m.NameTemplate)
		}
	}
	seenRef := map[string]bool{}
	for _, tmpl := range imageTmpls {
		ref, resolved := resolveTemplate(tmpl, version)
		if ref == "" || seenRef[ref] {
			continue
		}
		seenRef[ref] = true
		arts = append(arts, Artifact{
			Kind:    "ociImage",
			Name:    imageArtifactName(ref, len(seenRef) == 1),
			Ref:     ref,
			Source:  "goreleaser",
			Guessed: !resolved,
			Note:    "container image published by goreleaser",
		})
	}

	// Binaries: one artifact per build id, carrying its platform matrix.
	for _, b := range cfg.Builds {
		var platforms []string
		for _, os := range b.Goos {
			for _, arch := range b.Goarch {
				platforms = append(platforms, os+"/"+arch)
			}
		}
		sort.Strings(platforms)
		name := b.Binary
		if name == "" {
			name = b.ID
		}
		if name == "" {
			name = "binary"
		}
		arts = append(arts, Artifact{
			Kind:      "executable",
			Name:      "binary-" + name,
			Platforms: platforms,
			Source:    "goreleaser",
			Note:      binaryNote(name, platforms),
		})
	}
	return arts
}

// imageArtifactName returns "image" for the first discovered image and a
// registry-derived suffix for subsequent ones, keeping names stable and unique.
func imageArtifactName(ref string, first bool) string {
	if first {
		return "image"
	}
	// Use the last path segment of the repository (before any tag) as a suffix.
	repo := ref
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	seg := repo
	if i := strings.LastIndex(seg, "/"); i >= 0 {
		seg = seg[i+1:]
	}
	return "image-" + seg
}

func binaryNote(name string, platforms []string) string {
	if len(platforms) == 0 {
		return fmt.Sprintf("released binary %q published by goreleaser", name)
	}
	return fmt.Sprintf("released binary %q for %d platform(s) published by goreleaser", name, len(platforms))
}
