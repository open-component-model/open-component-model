package scanner

import (
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"
)

const (
	buildFileReadCap = 16 << 10 // 16 KiB
	koReadCap        = 4 << 10  // 4 KiB
)

// dockerPushRE matches a `docker push <ref>` command in a Makefile/Taskfile
// recipe, capturing a registry-qualified reference (dotted host).
var dockerPushRE = regexp.MustCompile(`docker\s+push\s+([a-z0-9.\-]+\.[a-z]{2,}/[^\s#$]+)`)

// koBuildRE detects a ko build/publish invocation, whose resulting image ref is
// derived from KO_DOCKER_REPO (an env/CI value, not statically known here).
var koBuildRE = regexp.MustCompile(`\bko\s+(build|publish|apply|resolve)\b`)

// taskfileConfig is the subset of a Taskfile we read: top-level vars (which
// commonly hold an IMAGE/IMG value) and task command lines.
type taskfileConfig struct {
	Vars  map[string]string `json:"vars"`
	Tasks map[string]struct {
		Cmds []string `json:"cmds"`
	} `json:"tasks"`
}

// buildSystemArtifacts scans Makefile, Taskfile.yml, and ko config for published
// container images and ko usage. It complements the docker scanner's Makefile
// IMG mining by also reading push commands and Taskfile image vars. version is
// currently unused for these sources (refs are literal) but kept for symmetry.
func buildSystemArtifacts(fs *FileSet, version string) []Artifact {
	_ = version
	var arts []Artifact
	seen := map[string]bool{}
	add := func(ref, source, note string) {
		ref = strings.TrimSpace(strings.Trim(ref, `"'`))
		if ref == "" || seen[ref] {
			return
		}
		seen[ref] = true
		arts = append(arts, Artifact{Kind: "ociImage", Ref: ref, Source: source, Note: note})
	}

	if b := fs.Read("Makefile", buildFileReadCap); b != nil {
		for _, m := range makeImgRE.FindAllStringSubmatch(string(b), -1) {
			if len(m) > 1 {
				add(m[1], "makefile", "container image built by the Makefile (IMG)")
			}
		}
		for _, m := range dockerPushRE.FindAllStringSubmatch(string(b), -1) {
			if len(m) > 1 {
				add(m[1], "makefile", "container image pushed by the Makefile")
			}
		}
		if koBuildRE.MatchString(string(b)) {
			arts = append(arts, koArtifact("makefile"))
		}
	}

	for _, name := range []string{"Taskfile.yml", "Taskfile.yaml"} {
		b := fs.Read(name, buildFileReadCap)
		if b == nil {
			continue
		}
		var cfg taskfileConfig
		if err := yaml.Unmarshal(b, &cfg); err == nil {
			for k, v := range cfg.Vars {
				ku := strings.ToUpper(k)
				if (strings.Contains(ku, "IMAGE") || ku == "IMG") && strings.Contains(v, "/") && strings.Contains(v, ".") {
					add(v, "taskfile", "container image referenced by a Taskfile var")
				}
			}
			for _, task := range cfg.Tasks {
				for _, cmd := range task.Cmds {
					for _, m := range dockerPushRE.FindAllStringSubmatch(cmd, -1) {
						if len(m) > 1 {
							add(m[1], "taskfile", "container image pushed by a Taskfile task")
						}
					}
					if koBuildRE.MatchString(cmd) {
						arts = append(arts, koArtifact("taskfile"))
					}
				}
			}
		}
	}

	// A ko config present at the root implies a ko-built image even without a
	// Makefile/Taskfile invocation.
	for _, name := range []string{".ko.yaml", "ko.yaml"} {
		if fs.Read(name, koReadCap) != nil {
			arts = append(arts, koArtifact("ko"))
			break
		}
	}

	return dedupArtifacts(arts)
}

// koArtifact is the ko-built image whose reference resolves from KO_DOCKER_REPO
// at build time and so cannot be known statically — flagged as a guess.
func koArtifact(source string) Artifact {
	return Artifact{
		Kind:    "ociImage",
		Ref:     "",
		Source:  source,
		Guessed: true,
		Note:    "container image built by ko (reference derives from KO_DOCKER_REPO at build time)",
	}
}

// dedupArtifacts removes artifacts that are exact (kind, ref, source) repeats
// and collapses multiple ko artifacts (empty ref) into one.
func dedupArtifacts(in []Artifact) []Artifact {
	seen := map[string]bool{}
	koSeen := false
	out := in[:0]
	for _, a := range in {
		if a.Kind == "ociImage" && a.Ref == "" {
			if koSeen {
				continue
			}
			koSeen = true
		}
		key := a.Kind + "|" + a.Ref + "|" + a.Source
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	return out
}
