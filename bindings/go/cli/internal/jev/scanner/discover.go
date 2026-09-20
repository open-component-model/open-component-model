package scanner

// DiscoverArtifacts gathers the CI/release outputs a repository publishes, from
// (in precedence order) goreleaser, GitHub Actions workflows, and build-system
// files (Makefile/Taskfile/ko). version resolves template variables in the
// discovered references. When several sources name the same image, the more
// authoritative source wins (goreleaser and CI over a Makefile var). The
// returned artifacts are additional to a component's primary deliverable and
// are deduplicated by (kind, reference).
func DiscoverArtifacts(fs *FileSet, version string) []Artifact {
	if fs == nil {
		return nil
	}
	var all []Artifact
	all = append(all, goreleaserArtifacts(fs, version)...)
	all = append(all, githubActionsArtifacts(fs, version)...)
	all = append(all, buildSystemArtifacts(fs, version)...)

	// Deduplicate images by reference, keeping the first (most authoritative)
	// occurrence; keep every distinct executable/archive.
	seenRef := map[string]bool{}
	koSeen := false
	out := make([]Artifact, 0, len(all))
	for _, a := range all {
		if a.Kind == "ociImage" {
			switch {
			case a.Ref == "": // unresolved (ko): keep at most one
				if koSeen {
					continue
				}
				koSeen = true
			case seenRef[a.Ref]:
				continue
			default:
				seenRef[a.Ref] = true
			}
		}
		out = append(out, a)
	}
	return out
}
