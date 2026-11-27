package testutils_test

import (
	"bufio"
	"errors"
	"fmt"
	"go/build/constraint"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	tags   []string
}

// buildConfigs covers every custom build constraint and OS-specific file set in
// bindings/go. TestBuildConfigsSelectEveryFile fails when a non-test file is not
// selected by any entry, so add an entry here when it tells you to.
var buildConfigs = []buildConfig{
	{name: "linux-amd64", goos: "linux", goarch: "amd64"},
	{name: "linux-amd64-pprof", goos: "linux", goarch: "amd64", tags: []string{"pprof"}},
	{name: "darwin-arm64", goos: "darwin", goarch: "arm64"},
	{name: "windows-amd64", goos: "windows", goarch: "amd64"},
}

// TestProductionBuildDoesNotImportTestutils lists the non-test import closure of
// every package in the module, once per build configuration in buildConfigs, and
// fails when a package outside the testutils tree reaches a testutils package.
//
// It queries `go list` with the .Deps field, which holds the transitive non-test
// imports (direct imports included). It does not include TestImports or
// XTestImports, so a test file that imports testutils is correctly ignored and
// only a non-test file that reaches testutils makes it fail.
//
// `go list` reads GOFLAGS from the environment, so a GOFLAGS=-tags=... setting
// from the parent also applies; the test cannot read the tags of the parent
// `go test` binary in a reliable way, so it does not try.
//
// See README.md in this directory for the full test-only policy.
func TestProductionBuildDoesNotImportTestutils(t *testing.T) {
	t.Parallel()
	for _, cfg := range buildConfigs {
		t.Run(cfg.name, func(t *testing.T) {
			t.Parallel()
			checkBuildConfig(t, cfg)
		})
	}
}

// TestBuildConfigsSelectEveryFile fails when a non-test file in the module is
// not part of the build for any entry in buildConfigs. Such a file is invisible
// to TestProductionBuildDoesNotImportTestutils, so a testutils import in it
// would go unnoticed.
func TestBuildConfigsSelectEveryFile(t *testing.T) {
	t.Parallel()

	knownOS, knownArch := knownPlatforms(t)
	root := moduleDir(t)

	var unselected []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		expr, err := buildConstraint(path)
		if err != nil {
			return err
		}
		fileOS, fileArch := fileNamePlatform(name, knownOS, knownArch)
		if !slices.ContainsFunc(buildConfigs, func(cfg buildConfig) bool {
			return cfg.selects(expr, fileOS, fileArch)
		}) {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			unselected = append(unselected, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(unselected) > 0 {
		sort.Strings(unselected)
		t.Errorf("no entry in buildConfigs selects these non-test files; add an entry that does:\n%s", strings.Join(unselected, "\n"))
	}
}

// selects reports whether a file with the given //go:build expression (nil if
// none) and file name GOOS/GOARCH suffix (empty if none) is part of the build
// for cfg.
func (cfg buildConfig) selects(expr constraint.Expr, fileOS, fileArch string) bool {
	if fileOS != "" && fileOS != cfg.goos {
		return false
	}
	if fileArch != "" && fileArch != cfg.goarch {
		return false
	}
	if expr == nil {
		return true
	}
	return expr.Eval(func(tag string) bool {
		switch {
		case tag == cfg.goos, tag == cfg.goarch, slices.Contains(cfg.tags, tag):
			return true
		case tag == "unix":
			return !slices.Contains([]string{"windows", "plan9", "js", "wasip1"}, cfg.goos)
		case tag == "gc", strings.HasPrefix(tag, "go1."):
			// The test always runs with the gc toolchain, and the module's go
			// directive is at least as new as any release tag a file can use.
			return true
		default:
			return false
		}
	})
}

// buildConstraint returns the //go:build expression in the file header, or nil
// if the file has none.
func buildConstraint(path string) (constraint.Expr, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "package ") {
			break
		}
		if constraint.IsGoBuild(line) {
			expr, err := constraint.Parse(line)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			return expr, nil
		}
	}
	return nil, scanner.Err()
}

// fileNamePlatform returns the GOOS and GOARCH that a file name suffix such as
// _windows.go or _linux_arm64.go implies, with the same rules as go/build.
func fileNamePlatform(name string, knownOS, knownArch map[string]bool) (goos, goarch string) {
	name = strings.TrimSuffix(name, ".go")
	i := strings.Index(name, "_")
	if i < 0 {
		return "", ""
	}
	l := strings.Split(name[i:], "_")
	n := len(l)
	if n >= 2 && knownOS[l[n-2]] && knownArch[l[n-1]] {
		return l[n-2], l[n-1]
	}
	if knownOS[l[n-1]] {
		return l[n-1], ""
	}
	if knownArch[l[n-1]] {
		return "", l[n-1]
	}
	return "", ""
}

// knownPlatforms returns every GOOS and GOARCH that the toolchain supports.
func knownPlatforms(t *testing.T) (knownOS, knownArch map[string]bool) {
	t.Helper()
	knownOS, knownArch = map[string]bool{}, map[string]bool{}
	for _, line := range runGo(t, nil, "tool", "dist", "list") {
		goos, goarch, ok := strings.Cut(line, "/")
		if !ok {
			t.Fatalf("unexpected `go tool dist list` output line: %q", line)
		}
		knownOS[goos], knownArch[goarch] = true, true
	}
	return knownOS, knownArch
}

// moduleDir returns the root directory of the bindings/go module.
func moduleDir(t *testing.T) string {
	t.Helper()
	lines := runGo(t, nil, "list", "-m", "-f", "{{.Dir}}")
	if len(lines) != 1 {
		t.Fatalf("unexpected `go list -m` output: %q", lines)
	}
	return lines[0]
}

// isTestutils reports whether pkg is the testutils package or one of its
// subpackages.
func isTestutils(pkg string) bool {
	return pkg == testutilsPkg || strings.HasPrefix(pkg, testutilsPkg+"/")
}

// checkBuildConfig lists the module for one build configuration and fails with
// every leaking package.
func checkBuildConfig(t *testing.T, cfg buildConfig) {
	t.Helper()

	args := []string{"list", "-f", `{{.ImportPath}}|{{join .Deps " "}}`}
	if len(cfg.tags) > 0 {
		args = append(args, "-tags="+strings.Join(cfg.tags, ","))
	}
	args = append(args, modulePattern)
	// CGO_ENABLED=0 keeps cross-GOOS listing from needing a C toolchain; `go list`
	// does not compile, so the listing stays fast.
	lines := runGo(t, []string{"GOOS=" + cfg.goos, "GOARCH=" + cfg.goarch, "CGO_ENABLED=0"}, args...)

	var leaks []string
	inspected := 0
	for _, line := range lines {
		pkg, deps, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("unexpected `go list` output line: %q", line)
		}
		if isTestutils(pkg) {
			continue
		}
		inspected++
		for _, dep := range strings.Fields(deps) {
			if isTestutils(dep) {
				leaks = append(leaks, fmt.Sprintf("%s reaches %s", pkg, dep))
			}
		}
	}

	if inspected == 0 {
		t.Fatalf("no packages to inspect; `go list %s` returned only the testutils tree", modulePattern)
	}

	if len(leaks) > 0 {
		sort.Strings(leaks)
		t.Errorf("production build reaches the test-only testutils tree:\n%s", strings.Join(leaks, "\n"))
	}
}

// runGo runs the go command with extra environment variables and returns its
// non-empty output lines.
func runGo(t *testing.T, env []string, args ...string) []string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Env = append(os.Environ(), env...)

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("go %s failed: %v\n%s", strings.Join(args, " "), err, exitErr.Stderr)
		}
		t.Fatalf("go %s failed: %v", strings.Join(args, " "), err)
	}

	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
