package testutils_test

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

const testutilsPkg = "ocm.software/open-component-model/bindings/go/internal/testutils"

const modulePattern = "ocm.software/open-component-model/bindings/go/..."

// TestProductionBuildDoesNotImportTestutils is defense in depth: it guarantees
// that no non-test build in the module can reach the testutils tree, and it does
// so without golangci. The "testutils-test-only" depguard rule in golangci.yml
// enforces the same policy at lint time, but a dropped or skipped lint run would
// silence it; this test keeps failing in `task bindings/go:test` and in CI.
//
// It relies on `go list -deps`, which walks only production imports (package
// Imports, not TestImports), so test files that import testutils are correctly
// ignored. The set of queried packages excludes the testutils tree itself, so
// testutils appears in the closure only when some non-test file outside the tree
// imports it, transitively or directly.
//
// The check is scoped to the current GOOS/GOARCH; files behind other build tags
// are covered by the depguard rule's GOOS=windows lint leg in CI.
func TestProductionBuildDoesNotImportTestutils(t *testing.T) {
	allPkgs := goList(t, modulePattern)

	query := make([]string, 0, len(allPkgs))
	for _, pkg := range allPkgs {
		if pkg == testutilsPkg || strings.HasPrefix(pkg, testutilsPkg+"/") {
			continue
		}
		query = append(query, pkg)
	}
	if len(query) == 0 {
		t.Fatalf("no packages to inspect; `go list %s` returned only the testutils tree", modulePattern)
	}

	deps := goList(t, append([]string{"-deps"}, query...)...)
	for _, dep := range deps {
		if dep == testutilsPkg || strings.HasPrefix(dep, testutilsPkg+"/") {
			t.Fatalf("production build reaches %q: some non-test file in the module imports the test-only testutils tree", dep)
		}
	}
}

// goList runs `go list` with the given arguments and returns the import paths it
// prints, one per line.
func goList(t *testing.T, args ...string) []string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", append([]string{"list"}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("go list %s failed: %v\n%s", strings.Join(args, " "), err, exitErr.Stderr)
		}
		t.Fatalf("go list %s failed: %v", strings.Join(args, " "), err)
	}

	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}
