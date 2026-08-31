package externalartifact

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// FuzzSingleFileName asserts that the derived filename is always a single,
// path-safe component regardless of the (attacker-influenceable) resource name.
func FuzzSingleFileName(f *testing.F) {
	for _, seed := range []string{
		"config", "podinfo", "", "..", "../../evil", "/etc/passwd",
		"a/b/c", "名前", "with space", "..\\..\\win", strings.Repeat("x", 300),
	} {
		f.Add(seed, "kustomization")
		f.Add(seed, "helmChart")
	}

	f.Fuzz(func(t *testing.T, name, typ string) {
		res := &descriptor.Resource{Type: typ}
		res.Name = name

		got := singleFileName(res)

		// Safety property: the result must be a single, safe path component — no
		// separators, not "."/"..", and it must not escape when joined to a base
		// dir. (Embedded dots like "0..yaml" are fine; only path components matter.)
		if got == "" {
			t.Fatalf("singleFileName(%q,%q) returned empty", name, typ)
		}
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("singleFileName(%q) = %q contains a path separator", name, got)
		}
		if got == "." || got == ".." {
			t.Errorf("singleFileName(%q) = %q is a path-traversal component", name, got)
		}
		if path.Base(got) != got {
			t.Errorf("singleFileName(%q) = %q is not a single component", name, got)
		}
		if joined := filepath.Join("/base", got); !strings.HasPrefix(joined, "/base/") {
			t.Errorf("singleFileName(%q) = %q escapes base dir: %q", name, got, joined)
		}
	})
}

// FuzzWriteTarballNoEscape builds a staging tree from fuzzed relative paths and
// asserts the resulting archive only contains entries under the packaged
// directory — i.e. writeTarball never emits an absolute or escaping path.
func FuzzWriteTarballNoEscape(f *testing.F) {
	f.Add("a.yaml")
	f.Add("nested/b.yaml")
	f.Add("deep/dir/c.txt")

	f.Fuzz(func(t *testing.T, rel string) {
		// Constrain to a plausible relative path; skip inputs that os.WriteFile
		// could not create so the fuzzer explores archive shapes, not FS errors.
		clean := filepath.Clean(rel)
		if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) || strings.ContainsRune(clean, 0) {
			t.Skip()
		}

		base := t.TempDir()
		full := filepath.Join(base, clean)
		if !strings.HasPrefix(full, base+string(filepath.Separator)) {
			t.Skip()
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Skip()
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Skip()
		}

		storage, err := NewStorage(t.TempDir(), "example.svc.")
		if err != nil {
			t.Fatalf("NewStorage: %v", err)
		}
		res, err := storage.Archive(t.Context(), "Resource", "ns", "name", base)
		if err != nil {
			t.Fatalf("Archive: %v", err)
		}

		for _, entry := range tarEntries(t, filepath.Join(storage.BasePath(), res.Path)) {
			if filepath.IsAbs(entry) || pathHasDotDotSegment(entry) {
				t.Errorf("archive entry %q is unsafe (from input %q)", entry, rel)
			}
		}
	})
}

// pathHasDotDotSegment reports whether any slash-separated segment of p is
// exactly ".." (a real traversal component), as opposed to a filename that
// merely contains dots (e.g. "0..0").
func pathHasDotDotSegment(p string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == ".." {
			return true
		}
	}

	return false
}

func tarEntries(t *testing.T, path string) []string {
	t.Helper()
	fh, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = fh.Close() }()

	gzr, err := gzip.NewReader(fh)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer func() { _ = gzr.Close() }()

	var names []string
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		names = append(names, hdr.Name)
	}

	return names
}
