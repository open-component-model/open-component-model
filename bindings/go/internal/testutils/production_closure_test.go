package testutils_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

const testutilsPkg = "ocm.software/open-component-model/bindings/go/internal/testutils"

const modulePattern = "ocm.software/open-component-model/bindings/go/..."

// buildConfig is one GOOS/GOARCH/tags combination that the closure test lists.
// A build constraint hides a file from the configurations that do not match it,
// so a leak in a _windows.go, a _darwin.go or a file behind a custom tag is only
// visible when a configuration selects that file.
type buildConfig struct {
	name   string
	goos   string
	goarch string
	tags   string
}

// buildConfigs covers every custom build constraint and OS-specific file set in
// bindings/go: the default build on Linux, the pprof tag, Darwin and Windows.
// When someone adds a new custom build tag or a new OS-specific file set in
// bindings/go, they must add an entry here so the closure test can see it.
var buildConfigs = []buildConfig{
	{name: "linux-amd64", goos: "linux", goarch: "amd64"},
	{name: "linux-amd64-pprof", goos: "linux", goarch: "amd64", tags: "pprof"},
	{name: "darwin-arm64", goos: "darwin", goarch: "arm64"},
	{name: "windows-amd64", goos: "windows", goarch: "amd64"},
}

// TestProductionBuildDoesNotImportTestutils lists the non-test import closure of
// every package in the module, once per build configuration in buildConfigs, and
// fails when a package outside the testutils tree reaches a testutils package.
//
// It queries `go list` with the .Imports and .Deps fields, which hold the direct
// and the transitive non-test imports. Neither field includes TestImports or
// XTestImports, so a test file that imports testutils is correctly ignored and
// only a non-test file that reaches testutils makes it fail.
//
// `go list` reads GOFLAGS from the environment, so a GOFLAGS=-tags=... setting
// from the parent also applies; the test cannot read the tags of the parent
// `go test` binary in a reliable way, so it does not try.
//
// See README.md in this directory for the full test-only policy.
func TestProductionBuildDoesNotImportTestutils(t *testing.T) {
	for _, cfg := range buildConfigs {
		t.Run(cfg.name, func(t *testing.T) {
			checkBuildConfig(t, cfg)
		})
	}
}

// isTestutils reports whether pkg is the testutils package or one of its
// subpackages.
func isTestutils(pkg string) bool {
	return pkg == testutilsPkg || strings.HasPrefix(pkg, testutilsPkg+"/")
}

// checkBuildConfig lists the module for one build configuration and fails with
// every leaking package, direct importers first.
func checkBuildConfig(t *testing.T, cfg buildConfig) {
	t.Helper()

	lines := goList(t, cfg)

	var direct, transitive []string
	inspected := 0
	for _, line := range lines {
		fields := strings.SplitN(line, "|", 3)
		if len(fields) != 3 {
			t.Fatalf("unexpected `go list` output line: %q", line)
		}
		pkg := fields[0]
		if isTestutils(pkg) {
			continue
		}
		inspected++

		directImports := make(map[string]bool)
		for _, imp := range strings.Fields(fields[1]) {
			if isTestutils(imp) {
				directImports[imp] = true
				direct = append(direct, fmt.Sprintf("%s imports %s", pkg, imp))
			}
		}
		for _, dep := range strings.Fields(fields[2]) {
			if isTestutils(dep) && !directImports[dep] {
				transitive = append(transitive, fmt.Sprintf("%s reaches %s (transitive)", pkg, dep))
			}
		}
	}

	if inspected == 0 {
		t.Fatalf("no packages to inspect; `go list %s` returned only the testutils tree", modulePattern)
	}

	sort.Strings(direct)
	sort.Strings(transitive)
	if len(direct) > 0 || len(transitive) > 0 {
		leaks := make([]string, 0, len(direct)+len(transitive))
		leaks = append(leaks, direct...)
		leaks = append(leaks, transitive...)
		t.Errorf("production build reaches the test-only testutils tree:\n%s", strings.Join(leaks, "\n"))
	}
}

// goList runs `go list` for one build configuration and returns its output
// lines, each formatted as "<import path>|<direct imports>|<transitive deps>".
func goList(t *testing.T, cfg buildConfig) []string {
	t.Helper()

	args := []string{"list", "-f", `{{.ImportPath}}|{{join .Imports " "}}|{{join .Deps " "}}`}
	if cfg.tags != "" {
		args = append(args, "-tags="+cfg.tags)
	}
	args = append(args, modulePattern)

	cmd := exec.CommandContext(t.Context(), "go", args...)
	// CGO_ENABLED=0 keeps cross-GOOS listing from needing a C toolchain; `go list`
	// does not compile, so the listing stays fast.
	cmd.Env = append(os.Environ(), "GOOS="+cfg.goos, "GOARCH="+cfg.goarch, "CGO_ENABLED=0")

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
