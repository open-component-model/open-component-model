// Package scanner turns a repository's walked file set into candidate OCM
// deliverables. Each ecosystem (Helm, Docker, Go, Node, Python, Rust, Java) has
// its own Scanner implementation that reads the shared FileSet and emits typed
// Candidates; a central composer ranks them by priority, picks the primary root
// deliverable, and drives monorepo fan-out. Adding an ecosystem is a new file
// plus one line in DefaultRegistry — no change to the composer or the walk.
package scanner

// FileSet is the shared, already-walked evidence a scanner reads. It is built
// once per repository by Walk and passed to every scanner, so N scanners do not
// cause N tree walks. All paths are repo-relative slash paths.
type FileSet struct {
	Root  string          // absolute repo root
	Files []string        // repo-relative slash paths of every walked file (bounded)
	Dirs  map[string]bool // repo-relative slash paths of every walked directory
	// fileSet is a membership index over Files, built by Walk for O(1) Has.
	fileSet map[string]bool
	// read returns up to capBytes of a repo-relative file, or nil on any error.
	read func(rel string, capBytes int) []byte
}

// Read returns up to capBytes of the repo-relative file, or nil. Read-only and
// nil-safe (a zero-value FileSet returns nil).
func (fs *FileSet) Read(rel string, capBytes int) []byte {
	if fs == nil || fs.read == nil {
		return nil
	}
	return fs.read(rel, capBytes)
}

// Has reports whether a repo-relative file path was walked.
func (fs *FileSet) Has(rel string) bool {
	if fs == nil {
		return false
	}
	return fs.fileSet[rel]
}

// Candidate is one deliverable a scanner proposes for a directory.
type Candidate struct {
	Dir        string   // repo-relative dir of the deliverable ("." for root)
	Kind       string   // OCM kind: ociImage | helmChart | goModule | npmPackage | pythonPackage | blob
	Priority   int      // higher wins when multiple candidates target the same Dir
	Ecosystem  string   // "go" | "helm" | "docker" | ... (for reports/labels)
	Note       string   // human-facing rationale, e.g. "detected a Chart.yaml"
	Runnable   bool     // deliverable is a runnable/executable (binary/image)
	ImageHints []string // discovered image references (registry-qualified), best first
	// TagPrefix is the version-namespace hint for this dir (e.g. the Helm chart
	// name for dash-delimited tags); "" means use the dir's basename default.
	TagPrefix string
}

// Scanner inspects a FileSet and returns the deliverables it recognises. A
// Scanner MUST be pure and side-effect free over the FileSet.
type Scanner interface {
	Name() string
	Scan(fs *FileSet) []Candidate
}

// Registry holds the ordered set of scanners. Registration order is the
// tie-break for candidates of equal priority targeting the same directory.
type Registry struct{ scanners []Scanner }

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Register appends a scanner to the registry.
func (r *Registry) Register(s Scanner) { r.scanners = append(r.scanners, s) }

// Scan runs every registered scanner over the file set and returns all
// candidates in registration order.
func (r *Registry) Scan(fs *FileSet) []Candidate {
	var all []Candidate
	for _, s := range r.scanners {
		all = append(all, s.Scan(fs)...)
	}
	return all
}

// Deliverable-kind priorities: the single source of truth for the ranking
// "image-serving chart > container image > runnable binary > language package >
// source library > opaque blob". Distinct constants keep kinds unambiguous so
// registration order only ever breaks genuine same-priority ties.
const (
	PriorityHelmChart = 100
	PriorityOCIImage  = 90
	PriorityBinary    = 70
	PriorityLangPkg   = 50
	PrioritySourceLib = 30
	PriorityBlob      = 10
)
