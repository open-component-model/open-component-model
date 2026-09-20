package scanner

import (
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

const workflowReadCap = 64 << 10 // 64 KiB

// workflow is the subset of a GitHub Actions workflow we read: the steps of
// every job, with the action ref (uses), inline run script, and `with:` inputs.
type workflow struct {
	Jobs map[string]struct {
		Steps []struct {
			Uses string            `json:"uses"`
			Run  string            `json:"run"`
			With map[string]string `json:"with"`
		} `json:"steps"`
	} `json:"jobs"`
}

// githubActionsArtifacts parses .github/workflows/*.yml and returns the release
// outputs the workflows publish: images pushed via docker/build-push-action
// (using docker/metadata-action images when tags are computed), and released
// binaries uploaded via gh release / softprops/action-gh-release. version
// resolves template variables. Returns nil when nothing publishable is found.
func githubActionsArtifacts(fs *FileSet, version string) []Artifact {
	var files []string
	for _, f := range fs.Files {
		s := strings.ToLower(f)
		if strings.Contains(s, ".github/workflows/") && (strings.HasSuffix(s, ".yml") || strings.HasSuffix(s, ".yaml")) {
			files = append(files, f)
		}
	}
	sort.Strings(files)

	var arts []Artifact
	seenRef := map[string]bool{}
	imageCount := 0
	shipsBinary := false

	for _, f := range files {
		b := fs.Read(f, workflowReadCap)
		if b == nil {
			continue
		}
		var wf workflow
		if err := yaml.Unmarshal(b, &wf); err != nil {
			continue
		}
		// Job iteration order is non-deterministic; collect metadata-action
		// images per workflow first so a later build-push step can reference them.
		var metaImages []string
		for _, job := range wf.Jobs {
			for _, st := range job.Steps {
				if actionIs(st.Uses, "docker/metadata-action") {
					metaImages = append(metaImages, splitList(st.With["images"])...)
				}
			}
		}
		for _, job := range wf.Jobs {
			for _, st := range job.Steps {
				switch {
				case actionIs(st.Uses, "docker/build-push-action"):
					if strings.EqualFold(st.With["push"], "false") {
						continue
					}
					for _, ref := range buildPushRefs(st.With["tags"], metaImages, version) {
						if ref.Ref == "" || seenRef[ref.Ref] {
							continue
						}
						seenRef[ref.Ref] = true
						imageCount++
						ref.Name = imageArtifactName(ref.Ref, imageCount == 1)
						arts = append(arts, ref)
					}
				case actionIs(st.Uses, "softprops/action-gh-release"), releasesBinary(st.Run):
					shipsBinary = true
				}
			}
		}
	}

	if shipsBinary {
		arts = append(arts, Artifact{
			Kind:    "executable",
			Name:    "binary",
			Source:  "github-actions",
			Guessed: true, // the exact released asset names are not statically known
			Note:    "released binary uploaded by a GitHub Actions release workflow",
		})
	}
	return arts
}

// buildPushRefs resolves the image references a build-push-action publishes from
// its `tags:` input. Each tag line is either a full ref (repo:tag) or a bare tag
// combined with the metadata-action images. Unresolved templates are flagged.
func buildPushRefs(tags string, metaImages []string, version string) []Artifact {
	var out []Artifact
	for _, raw := range splitList(tags) {
		t, resolved := resolveTemplate(strings.TrimSpace(raw), version)
		if t == "" {
			continue
		}
		if strings.Contains(t, "/") && strings.Contains(lastSeg(t), ":") {
			// A full registry-qualified ref with a tag.
			out = append(out, Artifact{Kind: "ociImage", Ref: t, Source: "github-actions", Guessed: !resolved, Note: "container image pushed by GitHub Actions"})
			continue
		}
		// A bare tag: combine with each metadata-action image.
		for _, img := range metaImages {
			img = strings.TrimSpace(img)
			if img == "" {
				continue
			}
			out = append(out, Artifact{Kind: "ociImage", Ref: img + ":" + t, Source: "github-actions", Guessed: !resolved, Note: "container image pushed by GitHub Actions"})
		}
	}
	return out
}

// actionIs reports whether a `uses:` value references the given action repo,
// ignoring the @version suffix and any leading docker:// style prefixes.
func actionIs(uses, action string) bool {
	if uses == "" {
		return false
	}
	if i := strings.Index(uses, "@"); i >= 0 {
		uses = uses[:i]
	}
	return strings.EqualFold(uses, action)
}

// releasesBinary reports whether a run script uploads release binaries via the
// gh CLI or goreleaser.
func releasesBinary(run string) bool {
	if run == "" {
		return false
	}
	return strings.Contains(run, "gh release ") || strings.Contains(run, "goreleaser release")
}

// splitList splits a YAML scalar that may hold newline- or comma-separated
// values (build-push tags/images are commonly multi-line block scalars).
func splitList(s string) []string {
	if s == "" {
		return nil
	}
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == ',' })
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if t := strings.TrimSpace(f); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// lastSeg returns the final slash-delimited segment of a path-like string.
func lastSeg(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}
