package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxFiles bounds the walk so a pathological repo cannot exhaust memory or time.
const maxFiles = 4000

// prune is the set of directory names never descended into during the walk.
var prune = map[string]bool{
	".git": true, ".claude": true, ".idea": true, "node_modules": true,
	"vendor": true, "dist": true, "target": true, ".venv": true,
	"__pycache__": true, "public": true, "resources": true,
}

// Walk builds a FileSet by walking root once, collecting every file and
// directory (repo-relative slash paths), pruning well-known non-source dirs and
// capping at maxFiles. It performs no language-specific probing — scanners do
// that over the returned FileSet. The read closure lazily reads bounded slices
// of files on demand.
func Walk(root string) *FileSet {
	absRoot, _ := filepath.Abs(root)
	files := make([]string, 0, 256)
	fileSet := make(map[string]bool, 256)
	dirs := map[string]bool{}
	n := 0

	_ = filepath.WalkDir(absRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries; best-effort evidence
		}
		rel, rerr := filepath.Rel(absRoot, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if p != absRoot && prune[d.Name()] {
				return filepath.SkipDir
			}
			dirs[rel] = true
			return nil
		}
		n++
		if n > maxFiles {
			return filepath.SkipAll
		}
		files = append(files, rel)
		fileSet[rel] = true
		return nil
	})
	sort.Strings(files)

	return &FileSet{
		Root:    absRoot,
		Files:   files,
		Dirs:    dirs,
		fileSet: fileSet,
		read: func(rel string, capBytes int) []byte {
			// rel is a repo-relative path we discovered during the walk; join it
			// back onto the absolute root and read a bounded prefix. Reading a
			// local repo the user pointed us at; read-only and size-capped.
			p := filepath.Join(absRoot, filepath.FromSlash(rel))
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			if capBytes > 0 && len(b) > capBytes {
				b = b[:capBytes]
			}
			return b
		},
	}
}

// NewFileSet builds an in-memory FileSet from a map of repo-relative slash paths
// to file contents. It exists for tests and callers that already hold file
// bytes; directories are derived from the file paths. read returns bounded
// slices of the provided contents.
func NewFileSet(root string, contents map[string]string) *FileSet {
	files := make([]string, 0, len(contents))
	fileSet := make(map[string]bool, len(contents))
	dirs := map[string]bool{".": true}
	for rel := range contents {
		files = append(files, rel)
		fileSet[rel] = true
		for d := dirOf(rel); d != "." && d != ""; d = dirOf(d) {
			dirs[d] = true
		}
	}
	sort.Strings(files)
	return &FileSet{
		Root:    root,
		Files:   files,
		Dirs:    dirs,
		fileSet: fileSet,
		read: func(rel string, capBytes int) []byte {
			s, ok := contents[strings.TrimPrefix(rel, "./")]
			if !ok {
				return nil
			}
			b := []byte(s)
			if capBytes > 0 && len(b) > capBytes {
				b = b[:capBytes]
			}
			return b
		},
	}
}
