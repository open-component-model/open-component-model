package scanner

import (
	"regexp"
	"strings"
)

// mainPackageRE matches a Go main entrypoint (a func main), used to distinguish
// a runnable binary from a source library.
var mainPackageRE = regexp.MustCompile(`(?m)^func\s+main\s*\(`)

const goFileReadCap = 4 << 10 // 4 KiB

// goScanner recognises Go modules by their go.mod and probes for a main
// entrypoint to distinguish a runnable binary from a source library.
type goScanner struct{}

func (goScanner) Name() string { return "go" }

func (goScanner) Scan(fs *FileSet) []Candidate {
	var out []Candidate
	for _, f := range fs.Files {
		if baseName(f) != "go.mod" {
			continue
		}
		modDir := dirOf(f)
		c := Candidate{Dir: modDir, Ecosystem: "go"}
		if goModuleRunnable(fs, modDir) {
			c.Kind = "blob"
			c.Priority = PriorityBinary
			c.Runnable = true
			c.Note = "Go module with a main entrypoint (a runnable binary)"
		} else {
			c.Kind = "goModule"
			c.Priority = PrioritySourceLib
			c.Note = "Go module with no main entrypoint (a source library)"
		}
		out = append(out, c)
	}
	return out
}

// goModuleRunnable reports whether the Go module rooted at modDir has a main
// entrypoint. It probes .go files in the module dir, its main/ subdir, and any
// cmd/** subdir, stopping at the first func main.
func goModuleRunnable(fs *FileSet, modDir string) bool {
	prefix := ""
	if modDir != "." {
		prefix = modDir + "/"
	}
	for _, f := range fs.Files {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		if !strings.HasPrefix(f, prefix) {
			continue
		}
		rel := strings.TrimPrefix(f, prefix) // path within the module
		d := dirOf(rel)
		if d == "." || d == "main" || d == "cmd" || strings.HasPrefix(d, "cmd/") {
			if b := fs.Read(f, goFileReadCap); b != nil && mainPackageRE.Match(b) {
				return true
			}
		}
	}
	return false
}
