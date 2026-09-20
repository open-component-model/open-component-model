package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	slogctx "github.com/veqryn/slog-context"

	"ocm.software/open-component-model/bindings/go/cli/internal/jev/scanner"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
)

// confAct is the confidence threshold at/above which a model choice/score is
// acted on automatically; below it, code falls back to the safest OCM modeling.
const confAct = 0.75

// jevManifestKinds maps a manifest filename to the deliverable-kind key used
// only for the Jev `state` JSON (DetectedManifests). Kind decisions are made by
// the scanner registry, not this map.
var jevManifestKinds = map[string]string{
	"go.mod":         "go_module",
	"Cargo.toml":     "rust_crate",
	"package.json":   "npm_package",
	"pyproject.toml": "python_dist",
	"setup.py":       "python_dist",
	"pom.xml":        "maven_artifact",
	"build.gradle":   "gradle_artifact",
	"Dockerfile":     "container_image",
	"Chart.yaml":     "helm_chart",
	"Makefile":       "make_build",
	"Taskfile.yml":   "task_build",
}

var langExts = map[string]bool{
	".go": true, ".rs": true, ".py": true, ".js": true, ".ts": true,
	".java": true, ".c": true, ".cpp": true, ".sh": true,
}

// RepoState is the compact deterministic evidence extracted from a repository,
// serialized as the Jev `state`.
type RepoState struct {
	RepoName           string              `json:"repo_name"`
	OriginURL          string              `json:"origin_url,omitempty"`
	Branch             string              `json:"branch,omitempty"`
	DetectedManifests  map[string][]string `json:"detected_manifests"`
	TopLevelDirs       []string            `json:"top_level_dirs"`
	LanguageFileCounts map[string]int      `json:"language_file_counts"`
	CIWorkflows        []string            `json:"ci_workflows"`
	TagTotal           int                 `json:"tag_count"`
	LatestTag          string              `json:"latest_tag,omitempty"`
	HeadCommit         string              `json:"head_commit,omitempty"`
	ReadmeExcerpt      string              `json:"readme_excerpt,omitempty"`
	License            string              `json:"license,omitempty"`

	fileset  *scanner.FileSet // unexported; the shared walked evidence
	versions *versionData     // unexported; populated by extractState via discoverVersions
}

// git runs `git -C root args...` and returns trimmed stdout, or "" on any error
// (missing git binary, non-repo, failed command). This lets repos without git
// still classify — versions/branch/origin simply become empty.
func git(ctx context.Context, root string, args ...string) string {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// root is a user-supplied local repository path and the git args are fixed
	// literals; shelling out to git is the documented, intentional mechanism.
	out, err := exec.CommandContext(cctx, "git", append([]string{"-C", root}, args...)...).Output() //nolint:gosec // G204: fixed git args, root is the user's own repo path
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// extractState walks root once (via the scanner FileSet) and gathers the
// deterministic evidence: the Jev-facing manifest/lang/dir/workflow summaries,
// README excerpt, detected license, plus git signals and version discovery
// (git tags only; no network). Kind decisions are made later by the scanner
// registry over the same FileSet.
func extractState(ctx context.Context, root string) (*RepoState, error) {
	fs := scanner.Walk(root)

	manifests := map[string][]string{}
	langs := map[string]int{}
	workflowSet := map[string]bool{}
	for _, rel := range fs.Files {
		name := filepath.Base(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		if kind, ok := jevManifestKinds[name]; ok {
			manifests[kind] = append(manifests[kind], rel)
		}
		ext := strings.ToLower(filepath.Ext(name))
		if langExts[ext] {
			langs[ext]++
		}
		if strings.Contains(dir, ".github/workflows") && (ext == ".yml" || ext == ".yaml") {
			workflowSet[name] = true
		}
	}

	// Cap and sort manifests (8/kind).
	for k, v := range manifests {
		sort.Strings(v)
		if len(v) > 8 {
			v = v[:8]
		}
		manifests[k] = v
	}

	var topDirs []string
	for d := range fs.Dirs {
		if d != "." && !strings.Contains(d, "/") {
			topDirs = append(topDirs, d)
		}
	}
	sort.Strings(topDirs)
	if len(topDirs) > 25 {
		topDirs = topDirs[:25]
	}

	workflows := make([]string, 0, len(workflowSet))
	for w := range workflowSet {
		workflows = append(workflows, w)
	}
	sort.Strings(workflows)
	if len(workflows) > 12 {
		workflows = workflows[:12]
	}

	tags := strings.Fields(git(ctx, root, "tag", "--sort=-creatordate"))
	var latestTag string
	if len(tags) > 0 {
		latestTag = tags[0]
	}

	state := &RepoState{
		RepoName:           filepath.Base(fs.Root),
		OriginURL:          git(ctx, root, "remote", "get-url", "origin"),
		Branch:             git(ctx, root, "rev-parse", "--abbrev-ref", "HEAD"),
		DetectedManifests:  manifests,
		TopLevelDirs:       topDirs,
		LanguageFileCounts: langs,
		CIWorkflows:        workflows,
		TagTotal:           len(tags),
		LatestTag:          latestTag,
		HeadCommit:         git(ctx, root, "rev-parse", "HEAD"),
		ReadmeExcerpt:      readmeExcerpt(fs),
		License:            detectLicense(fs),
		fileset:            fs,
		versions:           discoverVersions(ctx, root),
	}

	manifestKinds := make([]string, 0, len(state.DetectedManifests))
	for k := range state.DetectedManifests {
		manifestKinds = append(manifestKinds, k)
	}
	sort.Strings(manifestKinds)
	slogctx.FromCtx(ctx).Debug("init: extracted repository evidence",
		"root", fs.Root,
		"files_walked", len(fs.Files),
		"manifest_kinds", manifestKinds,
		"top_level_dirs", state.TopLevelDirs,
		"origin", state.OriginURL,
		"branch", state.Branch,
		"tags", state.TagTotal,
		"latest_tag", state.LatestTag,
		"license", state.License,
	)
	return state, nil
}

// readmeExcerpt returns the first 1500 bytes of a root README, or "".
func readmeExcerpt(fs *scanner.FileSet) string {
	for _, rel := range fs.Files {
		if !strings.Contains(rel, "/") && strings.HasPrefix(strings.ToLower(rel), "readme") {
			return strings.TrimSpace(string(fs.Read(rel, 1500)))
		}
	}
	return ""
}

// detectLicense inspects a root LICENSE file and returns a recognised SPDX id,
// or "" when there is no license file or the text is unrecognised.
func detectLicense(fs *scanner.FileSet) string {
	for _, name := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING"} {
		if b := fs.Read(name, 2048); b != nil {
			if id := licenseID(string(b)); id != "" {
				return id
			}
		}
	}
	return ""
}

// licenseID maps license header text to an SPDX identifier. Conservative: an
// unrecognised license yields "".
func licenseID(text string) string {
	switch {
	case strings.Contains(text, "Apache License") && strings.Contains(text, "2.0"):
		return "Apache-2.0"
	case strings.Contains(text, "MIT License"):
		return "MIT"
	case strings.Contains(text, "Mozilla Public License") && strings.Contains(text, "2.0"):
		return "MPL-2.0"
	case strings.Contains(text, "GNU GENERAL PUBLIC LICENSE") && strings.Contains(text, "Version 3"):
		return "GPL-3.0"
	case strings.Contains(text, "BSD 3-Clause") || strings.Contains(text, "Redistributions of source code"):
		return "BSD-3-Clause"
	default:
		return ""
	}
}

// -----------------------------------------------------------------------------
// Version discovery: git branch + namespaced tags + GitHub releases.
// Resolved per sub-deliverable with precedence release > tag > root > pseudo.
// -----------------------------------------------------------------------------

var (
	semverRE = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+.*)?$`)
	// tagRE matches bare or slash-namespaced tags: v1.2.3, services/api/v0.4.0.
	tagRE = regexp.MustCompile(`^(.*?/)?v?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.\-]+)?)$`)
	// dashTagRE matches Helm-style name-delimited tags: grafana-10.4.0,
	// agent-operator-0.10.0. The name (which may itself contain dashes) is the
	// namespace; the trailing semver is the version.
	dashTagRE = regexp.MustCompile(`^([A-Za-z][0-9A-Za-z._-]*?)-v?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.\-]+)?)$`)
)

// semverKey returns a sortable key for a semver string, ordering release above
// prerelease and numeric prerelease identifiers below alphanumeric ones. Non-
// semver strings sort below everything. Ported from the prototype semver_key.
type semverSortKey struct {
	major, minor, patch int
	releaseRank         int // 1 for release, 0 for prerelease
	preIDs              []preID
	invalid             bool
}

type preID struct {
	rank int // 0 = numeric, 1 = alphanumeric
	num  int
	str  string
}

func semverKey(v string) semverSortKey {
	m := semverRE.FindStringSubmatch(v)
	if m == nil {
		return semverSortKey{invalid: true}
	}
	maj, _ := strconv.Atoi(m[1])
	mnr, _ := strconv.Atoi(m[2])
	pat, _ := strconv.Atoi(m[3])
	pre := m[4]
	if pre == "" {
		return semverSortKey{major: maj, minor: mnr, patch: pat, releaseRank: 1}
	}
	var ids []preID
	for _, x := range strings.Split(pre, ".") {
		if isAllDigits(x) {
			nn, _ := strconv.Atoi(x)
			ids = append(ids, preID{rank: 0, num: nn})
		} else {
			ids = append(ids, preID{rank: 1, str: x})
		}
	}
	return semverSortKey{major: maj, minor: mnr, patch: pat, releaseRank: 0, preIDs: ids}
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// lessSemver reports whether a sorts before b (ascending: oldest first).
func lessSemver(a, b semverSortKey) bool {
	// Invalid versions sort below valid ones.
	if a.invalid != b.invalid {
		return a.invalid && !b.invalid
	}
	if a.major != b.major {
		return a.major < b.major
	}
	if a.minor != b.minor {
		return a.minor < b.minor
	}
	if a.patch != b.patch {
		return a.patch < b.patch
	}
	if a.releaseRank != b.releaseRank {
		return a.releaseRank < b.releaseRank // prerelease (0) < release (1)
	}
	// Both prerelease: compare identifier lists.
	for i := 0; i < len(a.preIDs) && i < len(b.preIDs); i++ {
		ai, bi := a.preIDs[i], b.preIDs[i]
		if ai.rank != bi.rank {
			return ai.rank < bi.rank // numeric (0) < alphanumeric (1)
		}
		if ai.rank == 0 {
			if ai.num != bi.num {
				return ai.num < bi.num
			}
		} else {
			if ai.str != bi.str {
				return ai.str < bi.str
			}
		}
	}
	return len(a.preIDs) < len(b.preIDs)
}

// splitTag splits a tag into (namespace prefix, version) or returns ok=false.
// It recognises three shapes: bare/slash-namespaced (v1.2.3, api/v0.4.0) and the
// Helm-style name-delimited form (grafana-10.4.0 -> namespace "grafana").
func splitTag(t string) (prefix, version string, ok bool) {
	if m := tagRE.FindStringSubmatch(t); m != nil {
		return strings.TrimRight(m[1], "/"), m[2], true
	}
	if m := dashTagRE.FindStringSubmatch(t); m != nil {
		return m[1], m[2], true
	}
	return "", "", false
}

type namespaceVersions struct {
	Latest       string
	LatestStable string
	Count        int
}

type versionData struct {
	Branch     string
	Namespaces map[string]namespaceVersions
}

// discoverVersions groups git tags into semver-sorted namespaces (no network).
// Ported from the prototype discover_versions.
func discoverVersions(ctx context.Context, root string) *versionData {
	ns := map[string][]string{}
	for _, t := range strings.Split(git(ctx, root, "tag"), "\n") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if pfx, ver, ok := splitTag(t); ok {
			ns[pfx] = append(ns[pfx], ver)
		}
	}
	out := map[string]namespaceVersions{}
	for pfx, vs := range ns {
		sorted := append([]string(nil), vs...)
		sort.SliceStable(sorted, func(i, j int) bool { return lessSemver(semverKey(sorted[i]), semverKey(sorted[j])) })
		var stable []string
		for _, v := range sorted {
			if !strings.Contains(v, "-") {
				stable = append(stable, v)
			}
		}
		var latestStable string
		if len(stable) > 0 {
			latestStable = stable[len(stable)-1]
		}
		out[pfx] = namespaceVersions{Latest: sorted[len(sorted)-1], LatestStable: latestStable, Count: len(vs)}
	}
	return &versionData{Branch: git(ctx, root, "rev-parse", "--abbrev-ref", "HEAD"), Namespaces: out}
}

var ownerRepoRE = regexp.MustCompile(`github\.com[:/]([^/]+)/(.+?)(?:\.git)?$`)

func parseOwnerRepo(originURL string) (owner, repo string, ok bool) {
	m := ownerRepoRE.FindStringSubmatch(originURL)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

type releaseRecord struct {
	Version    string
	Tag        string
	Prerelease bool
}

type releaseData struct {
	Available  bool
	Namespaces map[string][]releaseRecord
}

type ghRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

// discoverReleases fetches GitHub Releases for the repo behind originURL and
// groups them into version namespaces (same splitTag scheme). Best-effort: any
// error or non-github origin yields {Available:false}. Ported from the design
// doc's discover_releases contract. No GitHub SDK — net/http + encoding/json.
// The httpClient is the OCM-configured client so GitHub calls honour the same
// HTTP policy (retries, timeouts, TLS) as the rest of the CLI; when nil, the
// http binding's default client is used.
func discoverReleases(ctx context.Context, originURL string, httpClient *http.Client) *releaseData {
	if httpClient == nil {
		httpClient = ocmhttp.New()
	}
	owner, repo, ok := parseOwnerRepo(originURL)
	if !ok {
		return &releaseData{Available: false}
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=40", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return &releaseData{Available: false}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return &releaseData{Available: false}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &releaseData{Available: false}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &releaseData{Available: false}
	}
	var releases []ghRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return &releaseData{Available: false}
	}
	nsMap := map[string][]releaseRecord{}
	for _, r := range releases {
		if r.Draft {
			continue
		}
		pfx, ver, ok := splitTag(r.TagName)
		if !ok {
			continue
		}
		nsMap[pfx] = append(nsMap[pfx], releaseRecord{Version: ver, Tag: r.TagName, Prerelease: r.Prerelease})
	}
	// Sort each namespace ascending by semver so the last is newest.
	for pfx, recs := range nsMap {
		sort.SliceStable(recs, func(i, j int) bool {
			return lessSemver(semverKey(recs[i].Version), semverKey(recs[j].Version))
		})
		nsMap[pfx] = recs
	}
	return &releaseData{Available: true, Namespaces: nsMap}
}

// VersionResolution is the outcome of resolving a version for a sub-deliverable.
type VersionResolution struct {
	Version   string
	Source    string // github-release | git-tag | git-tag-root-fallback | pseudo | none
	Namespace string
	Tag       string
}

// resolveVersion resolves a version for the given subdir with precedence
// github-release > git-tag(namespace) > root tag > 0.0.0+<commit> pseudo.
// Ported from the prototype resolve_version + the pseudo-version fallback.
func resolveVersion(subdir string, vd *versionData, releases *releaseData, headCommit string) VersionResolution {
	seg := strings.Trim(subdir, "/")
	var parts []string
	if seg != "" {
		parts = strings.Split(seg, "/")
	}
	// Candidate namespaces: every path suffix, longest first.
	candSet := map[string]bool{}
	for i := range parts {
		candSet[strings.Join(parts[i:], "/")] = true
	}
	if len(parts) > 0 {
		candSet[parts[len(parts)-1]] = true
	}
	cands := make([]string, 0, len(candSet))
	for c := range candSet {
		if c != "" {
			cands = append(cands, c)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return len(cands[i]) > len(cands[j]) })
	// For the root component (empty subdir) the root namespace "" is a direct
	// candidate so it resolves from root-namespaced releases/tags. For a
	// non-empty subdir, root resolution is handled by the explicit
	// git-tag-root-fallback branch below (and the release loop cannot match "").
	if seg == "" {
		cands = append(cands, "")
	}

	if releases != nil && releases.Available {
		for _, c := range cands {
			recs, ok := releases.Namespaces[c]
			if !ok || len(recs) == 0 {
				continue
			}
			var stable []releaseRecord
			for _, r := range recs {
				if !r.Prerelease {
					stable = append(stable, r)
				}
			}
			pool := stable
			if len(pool) == 0 {
				pool = recs
			}
			pick := pool[len(pool)-1]
			return VersionResolution{Version: pick.Version, Source: "github-release", Namespace: c, Tag: pick.Tag}
		}
	}

	if vd != nil {
		for _, c := range cands {
			info, ok := vd.Namespaces[c]
			if !ok {
				continue
			}
			v := info.LatestStable
			if v == "" {
				v = info.Latest
			}
			tag := "v" + v
			if c != "" {
				tag = c + "/v" + v
			}
			return VersionResolution{Version: v, Source: "git-tag", Namespace: c, Tag: tag}
		}
		if info, ok := vd.Namespaces[""]; ok {
			v := info.LatestStable
			if v == "" {
				v = info.Latest
			}
			return VersionResolution{Version: v, Source: "git-tag-root-fallback", Namespace: ""}
		}
	}

	commit := headCommit
	if commit == "" {
		commit = "unknown"
	}
	if len(commit) > 12 {
		commit = commit[:12]
	}
	return VersionResolution{Version: "0.0.0+" + commit, Source: "pseudo"}
}

// -----------------------------------------------------------------------------
// Classification: two Jev waves + code-side composition. Wave 1 uses decomposed
// atomic capability nouls (design doc "Corpus evaluation"); code composes the
// primary OCM kind by priority (image > chart > binary/blob > lang package >
// source lib). Wave 2's modeling questions are gated by the wave-1 nouls.
// -----------------------------------------------------------------------------

// Decision is the composed classification outcome for a single component. It is
// produced deterministically by ClassifyRules (the default) and may be refined
// by the optional Jev enhancer. Wave1/Wave2 are only populated in AI mode.
type Decision struct {
	Kind             string // ociImage | helmChart | blob | goModule | npmPackage | pythonPackage
	Method           string // "rules" | "ai" — how the kind was decided, for reporting
	SourceAccess     string // github | git_generic | local_dir
	Provider         string // resolved provider name
	Maturity         string // experimental | active | stable
	VersioningScheme string // semver | prefixed_semver | calver | none
	ShipsContainer   bool
	ShipsHelm        bool
	IsMonorepo       bool
	AutoPublishable  bool
	MinConfidence    float64           // 1.0 for rules; calibrated model confidence in AI mode
	Notes            []string          // human-facing rationale for the report
	ImageHints       []string          // discovered image references (best first), assembler-consumed
	Wave1            map[string]Answer // raw wave-1 answers (AI mode only)
	Wave2            map[string]Answer // raw wave-2 answers (AI mode only)

	comp *scanner.Composition // unexported; monorepo fan-out + per-dir winners
}

// providerFromOrigin derives a provider name from the git origin: the GitHub
// org/owner for github origins, else "". Deterministic — no guessing.
func providerFromOrigin(originURL string) string {
	if owner, _, ok := parseOwnerRepo(originURL); ok {
		return "github.com/" + owner
	}
	return ""
}

// versioningSchemeFromTags infers the release versioning scheme from the tag
// namespaces: a non-root namespace present -> prefixed_semver; a bare semver
// root tag -> semver; a calendar-like latest tag -> calver; nothing -> none.
func versioningSchemeFromTags(state *RepoState) string {
	if state.versions == nil || len(state.versions.Namespaces) == 0 {
		if calverRE.MatchString(strings.TrimPrefix(state.LatestTag, "v")) {
			return "calver"
		}
		return "none"
	}
	for pfx := range state.versions.Namespaces {
		if pfx != "" {
			return "prefixed_semver"
		}
	}
	return "semver"
}

var calverRE = regexp.MustCompile(`^\d{4}[.\-]\d{1,2}`)

// maturityFromEvidence infers maturity conservatively from release tags: a
// stable (>=1.0.0, no prerelease) tag -> stable; any tag -> active; none ->
// experimental. It never overstates maturity.
func maturityFromEvidence(state *RepoState) string {
	if state.versions == nil {
		return "experimental"
	}
	hasStable, hasAny := false, false
	for _, info := range state.versions.Namespaces {
		if info.Count > 0 {
			hasAny = true
		}
		v := info.LatestStable
		if v != "" {
			if k := semverKey(v); !k.invalid && k.major >= 1 {
				hasStable = true
			}
		}
	}
	switch {
	case hasStable:
		return "stable"
	case hasAny:
		return "active"
	default:
		return "experimental"
	}
}

// composeState runs the scanner registry over the state's shared FileSet and
// returns the composition. The FileSet is populated by extractState; a
// zero-value FileSet (e.g. a hand-built state in a test) yields no candidates.
func composeState(state *RepoState) *scanner.Composition {
	fs := state.fileset
	if fs == nil {
		fs = &scanner.FileSet{}
	}
	return scanner.Compose(fs, scanner.DefaultRegistry().Scan(fs))
}

// ClassifyRules produces a Decision deterministically from the extracted
// evidence — no model, no network, no key. This is the default classifier: it
// is instant, offline, and reproducible. The primary kind and monorepo fan-out
// come from the scanner registry; genuinely guessed fields (an image reference
// with no discovered hint) are left as explicit TODOs by the assembler rather
// than fabricated here.
func ClassifyRules(state *RepoState) *Decision {
	comp := composeState(state)

	sourceAccess := "local_dir"
	if strings.HasPrefix(state.OriginURL, "http") && state.HeadCommit != "" {
		if _, _, ok := parseOwnerRepo(state.OriginURL); ok {
			sourceAccess = "github"
		} else {
			sourceAccess = "git_generic"
		}
	}

	provider := providerFromOrigin(state.OriginURL)
	if provider == "" {
		provider = "ocm.software"
	}

	notes := []string{comp.RootNote}
	if comp.IsMonorepo {
		notes = append(notes, fmt.Sprintf("detected %d sibling sub-deliverables and no root deliverable — treating as a monorepo", len(comp.SubDirs)))
	} else if state.fileset != nil && state.fileset.Has("go.work") {
		notes = append(notes, "a go.work file is present — treating workspace members as helpers of a single component, not separate deliverables")
	}
	if len(comp.ImageHints) > 0 {
		notes = append(notes, fmt.Sprintf("discovered image reference %q from the repository", comp.ImageHints[0]))
	}

	return &Decision{
		Kind:             comp.RootKind,
		Method:           "rules",
		SourceAccess:     sourceAccess,
		Provider:         provider,
		Maturity:         maturityFromEvidence(state),
		VersioningScheme: versioningSchemeFromTags(state),
		ShipsContainer:   comp.ShipsContainer,
		ShipsHelm:        comp.ShipsHelm,
		IsMonorepo:       comp.IsMonorepo,
		AutoPublishable:  true,
		MinConfidence:    1.0,
		Notes:            notes,
		ImageHints:       comp.ImageHints,
		comp:             comp,
	}
}

// conf returns an answer's confidence, defaulting to 1.0 when absent (matching
// the prototype _conf so the fallback path is exercised, not the calibrated one).
func conf(a Answer) float64 {
	if a.Confidence == 0 {
		return 1.0
	}
	return a.Confidence
}

// pick returns the answer's choice when its confidence meets the activation
// threshold, else the safe fallback. Ported from the prototype _pick.
func pick(a Answer, fallback string, threshold float64) string {
	if conf(a) >= threshold {
		return a.Choice
	}
	return fallback
}

// capQuestions are the decomposed atomic capability nouls plus the metadata
// questions issued in Wave 1. Instructions are load-bearing (they drive Jev's
// calibration) and are copied from the design/prototype wording.
func wave1Questions() map[string]Question {
	return map[string]Question{
		"ships_container":   {Type: "noul", Instructions: "Does this repository build and publish a container/OCI image as a first-class deliverable (not merely a test fixture or a dev container)?"},
		"ships_helm":        {Type: "noul", Instructions: "Does this repository ship a Helm chart as a deliverable (under deploy/ or chart/), not just a testdata/ fixture?"},
		"ships_binary":      {Type: "noul", Instructions: "Does this repository produce an executable binary/CLI as a primary deliverable that users run directly?"},
		"is_lang_package":   {Type: "noul", Instructions: "Is this repository primarily a language package published to a package registry (npm, PyPI, Go module, crate)?"},
		"is_source_library": {Type: "noul", Instructions: "Is this repository primarily a source library/SDK consumed by importing its source, rather than a runnable artifact?"},
		"is_monorepo":       {Type: "noul", Instructions: "Does this repository hold multiple independently-versioned or independently-deployable components (a monorepo)?"},
		"maturity": {
			Type: "score", Instructions: "How production-mature is this repository, judging from CI depth, release tags, and README claims?",
			Criteria: []string{"experimental or prototype", "actively developed, pre-1.0", "stable / production-grade"},
		},
		"versioning_scheme": {
			Type: "choice", Instructions: "What release versioning scheme does this project use? Judge from the latest tag and tag patterns.",
			Criteria: map[string]string{
				"semver":          "v1.2.3 / 1.2.3",
				"prefixed_semver": "component-prefixed like website/v0.16.0",
				"calver":          "calendar like 2026.05",
				"none":            "no discernible release versioning",
			},
		},
		"auto_publishable": {
			Type: "noul", Instructions: "Is this component well-characterized enough to auto-produce an OCM component version WITHOUT human review? true: clear single purpose+provider+release scheme; false: ambiguous, needs human confirmation.",
			Criteria: map[string]string{"true": "clear single purpose+provider+release scheme", "false": "ambiguous; needs human confirm"},
		},
	}
}

// hasManifest reports whether the state detected any manifest of the given kind.
func (s *RepoState) hasManifest(kind string) bool {
	return len(s.DetectedManifests[kind]) > 0
}

// composeKind selects the primary OCM resource kind from the wave-1 capability
// nouls by priority (image > chart > binary/blob > language package > source
// library), consulting detected manifests to disambiguate the language package.
// Ported from the design doc's compose_kind.
func composeKind(s *RepoState, w1 map[string]Answer) string {
	noul := func(k string) float64 { return w1[k].Noul }
	switch {
	case noul("ships_container") >= 0.5:
		return "ociImage"
	case noul("ships_helm") >= 0.5 && s.hasManifest("helm_chart"):
		return "helmChart"
	case noul("ships_binary") >= 0.5:
		return "blob"
	case noul("is_lang_package") >= 0.5:
		switch {
		case s.hasManifest("npm_package"):
			return "npmPackage"
		case s.hasManifest("python_dist"):
			return "pythonPackage"
		case s.hasManifest("go_module"):
			return "goModule"
		case s.hasManifest("rust_crate"):
			return "blob"
		default:
			return "blob"
		}
	case noul("is_source_library") >= 0.5 && s.hasManifest("go_module"):
		return "goModule"
	default:
		return "blob"
	}
}

// Classify runs the two Jev waves against the state and composes a Decision.
// threshold is the confidence activation floor (defaults to confAct when 0).
func Classify(ctx context.Context, client *Client, state *RepoState, threshold float64) (*Decision, error) {
	if threshold <= 0 {
		threshold = confAct
	}
	w1, err := client.Ask(ctx, state, wave1Questions())
	if err != nil {
		return nil, fmt.Errorf("wave 1 classification: %w", err)
	}

	shipsContainer := w1["ships_container"].Noul >= 0.5
	shipsHelm := w1["ships_helm"].Noul >= 0.5 && state.hasManifest("helm_chart")

	q2 := map[string]Question{
		"source_access": {
			Type: "choice", Instructions: "How should the SOURCE of this component best be referenced in an OCM component version?",
			Criteria: map[string]string{
				"github":      "GitHub repo at a commit/ref (access 'GitHub').",
				"git_generic": "generic git remote (source type 'git').",
				"local_dir":   "bundle the working tree as a Dir blob input.",
			},
		},
		"provider_kind": {
			Type: "choice", Instructions: "Who is the natural PROVIDER of this component, judging from origin and README?",
			Criteria: map[string]string{
				"open_source_org": "a named OSS org/project.",
				"individual":      "an individual developer.",
				"vendor":          "a commercial vendor.",
			},
		},
	}
	if shipsContainer {
		q2["image_access"] = Question{
			Type: "choice", Instructions: "For the container image this repo produces, how is it most naturally addressed as an OCM resource?",
			Criteria: map[string]string{
				"oci_registry": "published to an OCI registry (access 'OCIImage').",
				"local_build":  "built locally and embedded as a local blob.",
			},
		}
	}
	if shipsHelm {
		q2["helm_input"] = Question{
			Type: "choice", Instructions: "For the Helm chart this repo ships, how should it enter the component version?",
			Criteria: map[string]string{
				"packaged_dir": "package the chart directory (input 'Helm', path).",
				"helm_repo":    "reference from a Helm repository (input 'Helm', helmRepository).",
			},
		}
	}
	w2, err := client.Ask(ctx, state, q2)
	if err != nil {
		return nil, fmt.Errorf("wave 2 classification: %w", err)
	}

	// Minimum choice/score confidence across acted-on decisions, for observability.
	minConf := 1.0
	for _, k := range []string{"versioning_scheme"} {
		if c := conf(w1[k]); c < minConf {
			minConf = c
		}
	}
	if c := conf(w1["maturity"]); c < minConf {
		minConf = c
	}
	for _, a := range w2 {
		if c := conf(a); c < minConf {
			minConf = c
		}
	}

	maturityLevels := []string{"experimental", "active", "stable"}
	mi := int(w1["maturity"].Score + 0.5)
	if mi < 0 {
		mi = 0
	}
	if mi >= len(maturityLevels) {
		mi = len(maturityLevels) - 1
	}

	maturity := maturityLevels[mi]

	// Start from the deterministic baseline, then let confident model answers
	// refine the genuinely judgemental fields (kind, monorepo, maturity, source,
	// provider). Evidence-backed facts the rules already know stay authoritative.
	dec := ClassifyRules(state)
	dec.Method = "ai"
	dec.Wave1 = w1
	dec.Wave2 = w2
	dec.MinConfidence = minConf
	dec.ShipsContainer = shipsContainer
	dec.ShipsHelm = shipsHelm
	dec.Kind = composeKind(state, w1)
	dec.IsMonorepo = w1["is_monorepo"].Noul >= 0.5
	dec.Maturity = maturity
	dec.VersioningScheme = pick(w1["versioning_scheme"], dec.VersioningScheme, threshold)
	dec.AutoPublishable = w1["auto_publishable"].Noul >= 0.5
	if sa := pick(w2["source_access"], "", threshold); sa != "" {
		dec.SourceAccess = sa
	}
	dec.Notes = append(dec.Notes, "kind and monorepo status refined by the Jev model")
	return dec, nil
}
