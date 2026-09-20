package scanner

import (
	"regexp"
	"sort"
	"strings"
)

// noisePathRE matches directories that are build scaffolding, tooling, tests, or
// docs rather than shippable sub-deliverables. Tuned against real repos:
// ocm-controller's component/ and deploy/ are chart/build scaffolding for one
// component; prometheus's internal/tools and web/ui are tooling, not products.
var noisePathRE = regexp.MustCompile(`(^|/)(testdata|test|tests|fixtures?|examples?|hack|scripts|\.github|docs?|e2e|integration|node_modules|vendor|internal|tools|build|dist|deploy|config|component|components|web)(/|$)`)

// Composition is the composed view over all scanner candidates: the primary
// (dominant) deliverable, the monorepo fan-out decision, and per-directory
// winners for sub-deliverable assembly.
type Composition struct {
	Dominant       Candidate // repo-wide highest-priority deliverable
	RootKind       string    // Dominant.Kind, or "blob" when nothing was recognised
	RootNote       string    // rationale for the dominant kind
	Runnable       bool      // dominant deliverable is a runnable/executable
	ShipsContainer bool      // any candidate anywhere is a container image
	ShipsHelm      bool      // any candidate anywhere is a helm chart
	ImageHints     []string  // image hints from the dominant candidate
	IsMonorepo     bool
	SubDirs        []string             // sub-deliverable dirs (monorepo), sorted, collapsed
	ByDir          map[string]Candidate // best candidate per dir
}

// Compose ranks candidates, picks the primary (dominant) deliverable repo-wide,
// and decides monorepo fan-out. It is the deterministic replacement for the old
// classifyKindRules / rootHasDeliverable / isMonorepoRules / discoverSubdeliverables.
//
// The dominant kind is chosen repo-wide by priority (a chart in deploy/ outranks
// a Dockerfile at the root, matching the original repo-wide priority switch),
// while monorepo fan-out is gated separately on whether the root itself carries
// a deliverable manifest.
func Compose(fs *FileSet, cands []Candidate) *Composition {
	byDir := bestByDir(cands)

	comp := &Composition{ByDir: byDir, RootKind: "blob"}
	for _, c := range cands {
		if c.Ecosystem == "docker" {
			comp.ShipsContainer = true
		}
		if c.Kind == "helmChart" {
			comp.ShipsHelm = true
		}
	}

	if dom, ok := dominant(cands); ok {
		comp.Dominant = dom
		comp.RootKind = dom.Kind
		comp.RootNote = dom.Note
		comp.Runnable = dom.Runnable
		comp.ImageHints = dom.ImageHints
	} else {
		comp.RootNote = "no specific deliverable manifest detected; defaulting to a packaged directory"
	}

	// Sub-deliverable directories: every best-per-dir candidate that is not the
	// root and not a noise path, collapsed so a child folds into an ancestor.
	var subs []string
	for dir := range byDir {
		if dir == "." || noisePathRE.MatchString(dir) {
			continue
		}
		subs = append(subs, dir)
	}
	sort.Strings(subs)
	comp.SubDirs = collapseChildren(subs)

	// Monorepo only when the root is not itself a deliverable, there are at least
	// two genuine sibling sub-deliverables, and there is no go.work at the root
	// (a Go workspace's members are helpers, not independent products).
	_, rootIsDeliverable := byDir["."]
	comp.IsMonorepo = !rootIsDeliverable && len(comp.SubDirs) >= 2 && !fs.Has("go.work")

	return comp
}

// dominant returns the repo-wide highest-priority candidate; ties are broken by
// the earlier candidate (i.e. earlier-registered scanner, then walk order).
func dominant(cands []Candidate) (Candidate, bool) {
	var best Candidate
	found := false
	for _, c := range cands {
		if !found || c.Priority > best.Priority {
			best, found = c, true
		}
	}
	return best, found
}

// bestByDir keeps, per directory, the highest-priority candidate; ties are
// broken by the earlier candidate (i.e. earlier-registered scanner).
func bestByDir(cands []Candidate) map[string]Candidate {
	byDir := make(map[string]Candidate, len(cands))
	for _, c := range cands {
		if cur, ok := byDir[c.Dir]; !ok || c.Priority > cur.Priority {
			byDir[c.Dir] = c
		}
	}
	return byDir
}

// collapseChildren folds a child directory into an ancestor already present in
// the (sorted) list, so charts/a and charts/a/sub yield only charts/a.
func collapseChildren(sorted []string) []string {
	var out []string
	for _, d := range sorted {
		child := false
		for _, parent := range out {
			if strings.HasPrefix(d, parent+"/") {
				child = true
				break
			}
		}
		if !child {
			out = append(out, d)
		}
	}
	return out
}
