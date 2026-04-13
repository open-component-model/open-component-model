# jsonschemagen Performance Analysis

## Executive Summary

The `jsonschemagen` tool generates JSON Schema Draft 2020-12 definitions from Go types
annotated with `+ocm:jsonschema-gen` markers. It currently takes **6.5s (warm cache)** to
**21.7s (cold cache/CI)** due to three root causes:

1. **`go run` compilation overhead** — the Taskfile uses `go run` instead of a pre-built binary
2. **Excessive package loading** — 85% of loaded packages contain no annotated types
3. **Double `packages.Load` invocations** — marker scanning loads packages once, then full loading does it again

A pre-built binary with targeted package loading could reduce this to **<0.5s**.

---

## Measured Timings (Local Machine)

| Scenario | Wall Time | User Time | Notes |
|---|---|---|---|
| `go run` (cold cache) | **21.7s** | 5m42s | CI scenario without build cache |
| `go run` (warm cache) | **6.5s** | 1m27s | Local dev with build cache |
| `go run` (hot cache) | **0.7s** | 7.5s | Immediately after first run |
| Pre-built binary | **0.4s** | 3.9s | Runtime-only, no compilation |
| `go build` alone | **0.15s** | 0.2s | Compiling the binary (warm cache) |

**Key insight**: The actual schema generation logic (Phase 3) takes ~14ms. Nearly all time is
spent in Go toolchain compilation and `golang.org/x/tools/go/packages` loading.

---

## Architecture Overview

```text
main.go (cmd/main.go)
    │
    ├── Phase 1: Module Discovery (findModuleRoots)
    │   └── filepath.WalkDir to find all go.mod files
    │   └── Found: 28 modules
    │   └── Time: ~1ms
    │
    ├── Phase 2: Marker Scanning (findModulesWithSchemaMarkers)
    │   └── For EACH of 28 modules:
    │       └── packages.Load(NeedFiles, "./...")  ← EXPENSIVE
    │       └── Scan each .go file for "+ocm:jsonschema-gen" string
    │   └── Found: 15 modules with markers
    │   └── Time: ~100ms (warm) / ~500ms (cold)
    │
    ├── Phase 3: Full Package Loading (LoadTargets)
    │   └── For EACH of 15 modules + runtime:
    │       └── packages.Load(NeedSyntax|NeedTypes|NeedTypesInfo|NeedFiles|NeedImports, "./...")
    │   └── Loaded: 190 packages, 554 types
    │   └── Time: ~300ms (warm) / ~5s (cold) ← DOMINANT COST
    │
    ├── Phase 4: Schema Generation
    │   └── Filter 81 annotated types
    │   └── For each: GenerateJSONSchemaDraft202012()
    │   └── Write 79 schema files + 27 embed files
    │   └── Time: ~14ms
    │
    └── Total binary runtime: ~400ms warm / ~6s cold
        + go run compilation: ~150ms warm / ~16s cold
```

---

## Bottleneck Analysis

### Bottleneck 1: `go run` Compilation (HIGH Impact)

**File**: `bindings/go/generator/Taskfile.yml:49`

```yaml
jsonschemagen/generate:
  cmd: 'go run {{ .TASKFILE_DIR }}/jsonschemagen/cmd/main.go {{ .CLI_ARGS }} {{ .ROOT_DIR }}'
```

`go run` recompiles the tool every invocation. On CI without a build cache, this adds
**~16-20 seconds** of pure compilation time (compiling `golang.org/x/tools/go/packages` and
its transitive dependencies).

**Compare with `ocmtypegen`**: The sibling generator uses a pre-install step:
```yaml
ocmtypegen/install:
  cmd: go install {{ .PKG }}
ocmtypegen/generate:
  deps: ["ocmtypegen/install"]
```

**Fix**: Add a build/install step for jsonschemagen, matching the ocmtypegen pattern.
Expected improvement: **-16s on cold cache, -0.3s on warm cache**.

---

### Bottleneck 2: Whole-Module Loading (HIGH Impact)

**File**: `universe/build.go:171-172`

```go
cfg.Dir = target.Path
pkgs, err = packages.Load(cfg, "./...")
```

When a module has even ONE file with a marker, ALL packages in that module are loaded with
full type-checking (`NeedSyntax|NeedTypes|NeedTypesInfo`). This is extremely wasteful:

| Module | Packages Loaded | Annotated Packages | Waste |
|---|---|---|---|
| cli | 55 | 1 | **98%** |
| oci | 37 | 5 | 86% |
| plugin | 34 | 1 | **97%** |
| helm | 12 | 3 | 75% |
| blob | 10 | 1 | 90% |
| transform | 9 | 1 | 89% |
| **Total** | **190** | **28** | **85%** |

The `cli` module alone loads 55 packages (including the entire CLI framework, cobra commands,
integration test packages) just because ONE package
(`cli/internal/plugin/spec/config/v2alpha1`) has a marker.

**Fix**: Load only the specific packages that contain markers, not entire modules.
Phase 2 already identifies which files have markers — use that information to load individual
packages by import path instead of `./...`.

Expected improvement: Load ~28-35 packages instead of 190 (**~80% reduction** in package loading).

---

### Bottleneck 3: Double `packages.Load` for Marker Detection (MEDIUM Impact)

**File**: `universe/build.go:330-337`

```go
func moduleHasSchemaMarkers(ctx context.Context, modRoot, marker string) (bool, error) {
    cfg := &packages.Config{
        Mode: packages.NeedFiles,  // packages.Load just for file listing!
    }
    allPkgs, err := packages.Load(cfg, "./...")
```

For each of 28 modules, `packages.Load(NeedFiles)` is called just to get the list of `.go`
files. This invokes `go list` under the hood, which is expensive. The function then scans
each file for the marker string using `bufio.Scanner`.

**The code comment itself says**: "Use go list to discover all packages (much faster than file
walking)" — but this is incorrect. `filepath.WalkDir` + `strings.Contains` on `.go` files is
**~30ms** for the entire repository, while 28 `packages.Load(NeedFiles)` calls take **~100ms+**.

**Fix**: Replace `packages.Load(NeedFiles)` with direct `filepath.WalkDir` scanning for `.go`
files containing the marker. This was already partially done (the `fileContainsSchemaMarker`
function reads files directly), but it's unnecessarily wrapped in a `packages.Load` call.

Measured: `grep -rl "+ocm:jsonschema-gen" --include="*.go" .` completes in **32ms** for the
entire repo.

Expected improvement: **-70-100ms** per invocation, eliminates 28 `go list` subprocess spawns.

---

### Bottleneck 4: Sequential Schema Generation (LOW Impact)

**File**: `cmd/main.go:92-110`

```go
for _, ti := range annotated {
    schema := gen.GenerateJSONSchemaDraft202012(ti)
    writer.WriteSchemaJSON(ti, schema)
}
```

Schema generation and writing happens sequentially. With 81 types this takes ~14ms total, so
parallelism would save negligible time. However, if the number of annotated types grows
significantly, this could become relevant.

**Fix**: Not needed now. The generation loop is I/O-bound on file writes, and each schema
generation is CPU-cheap (~0.17ms per type).

---

### Bottleneck 5: Redundant Marker Extraction (LOW Impact)

**File**: `marker.go:315-322` and `cmd/main.go:93`

```go
// Called TWICE per annotated type in the main loop:
SchemaFromUniverseType(ti)  // line 93 — extracts markers
gen.GenerateJSONSchemaDraft202012(ti)  // line 99 — extracts markers again inside
```

`ExtractMarkerMap` is called multiple times for the same type during generation. The function
parses comment strings each time. This is fast (~microseconds per call) but wasteful.

**Fix**: Cache marker maps per TypeInfo. Low priority since total overhead is negligible.

---

## CI-Specific Analysis

### Current CI Configuration (`.github/workflows/ci.yml:340-365`)

```yaml
generate:
  steps:
    - uses: actions/setup-go@v6
      with:
        go-version-file: bindings/go/generator/go.mod
        cache-dependency-path: bindings/go/generator/go.sum
    - run: task generate  # Runs ALL generators sequentially
```

**Issues**:

1. `cache-dependency-path` only caches the generator module's dependencies, NOT the dependencies
   of the 15 target modules that `packages.Load` needs to resolve
2. `task generate` runs 6 generators sequentially (ocmtypegen, jsonschemagen, deepcopy-gen,
   manifests, controller generate, CLI docs)
3. The `ocmtypegen` runs first and could share universe data with `jsonschemagen`, but they
   are separate processes

---

## Recommended Improvements (Ordered by Impact)

### 1. Pre-build the binary (Estimated: -16s cold / -0.3s warm)

Change `Taskfile.yml` to match the `ocmtypegen` pattern:

```yaml
jsonschemagen/install:
  desc: "Build jsonschemagen binary"
  env:
    GOBIN: '{{ .ROOT_DIR }}/tmp/bin'
  cmds:
    - go install {{ .TASKFILE_DIR }}/jsonschemagen/cmd/...

jsonschemagen/generate:
  deps: ["jsonschemagen/install"]
  cmd: '{{ .ROOT_DIR }}/tmp/bin/jsonschemagen {{ .CLI_ARGS }} {{ .ROOT_DIR }}'
```

**Risk**: None. This is a strictly better invocation pattern.

### 2. Load only annotated packages, not entire modules (Estimated: -60-80% of load time)

Replace the current flow:

```text
discover modules → check which modules have markers → load entire modules
```

With:

```text
discover modules → find specific files with markers → derive package import paths → load only those packages
```

This requires changes to `universe/build.go`:

- `discoverLoadTargets` should return per-package targets, not per-module
- `loadTarget` should accept individual import paths (it already supports `LoadTargetTypeImport`)
- Each annotated package needs its module's `Dir` set in the config for resolution

Alternatively, use `packages.Load(cfg, "pkg1", "pkg2", "pkg3", ...)` with all annotated
package paths in a single call per module, which would let the Go toolchain batch-resolve them.

**Risk**: Low. Types referenced from annotated types in other packages within the same module
would need to be resolved through `NeedImports` transitive loading. This should work
automatically since `packages.Load` resolves imports transitively when `NeedTypes` is set.

### 3. Replace packages.Load(NeedFiles) with filepath.WalkDir (Estimated: -70-100ms)

In `moduleHasSchemaMarkers`, replace:
```go
cfg := &packages.Config{Mode: packages.NeedFiles}
allPkgs, err := packages.Load(cfg, "./...")
```

With:
```go
filepath.WalkDir(modRoot, func(p string, d fs.DirEntry, err error) error {
    if !d.IsDir() && strings.HasSuffix(p, ".go") {
        if found, _ := fileContainsSchemaMarker(p, marker); found {
            // record this directory as containing a marker
        }
    }
    return nil
})
```

This eliminates 28 `go list` subprocess spawns in Phase 2.

**Risk**: None. The `packages.Load(NeedFiles)` call was only used to enumerate `.go` files.
Direct file walking achieves the same result faster.

### 4. Improve CI caching (Estimated: 5-10s improvement on cache miss)

The `setup-go` action currently only caches `bindings/go/generator/go.sum`. But `packages.Load`
needs to resolve types from ALL target modules. Adding their go.sum files ensures the Go module
cache is populated:

```yaml
cache-dependency-path: |
  bindings/go/generator/go.sum
  bindings/go/blob/go.sum
  bindings/go/configuration/go.sum
  bindings/go/credentials/go.sum
  # ... etc
```

Or use a glob: `**/go.sum`.

**Risk**: Larger cache size, slower cache restore. May not be worth it if improvements 1-3 are
implemented.

### 5. ~~Parallelize generators in task generate~~ — NOT SAFE

`ocmtypegen` and `jsonschemagen` share 27 of 28 target directories. While they write to
different files (`zz_generated.ocm_type.go` vs `zz_generated.ocm_jsonschema.go`),
`jsonschemagen` uses `packages.Load(NeedSyntax)` which parses ALL `.go` files in each
package — including `zz_generated.ocm_type.go`. Running both generators concurrently would
create a race condition where `jsonschemagen` reads partially-written `ocmtypegen` output.

The sequential ordering in `Taskfile.yml` is correct and must be preserved.

---

## File Reference

| File | Lines | Purpose |
|---|---|---|
| `cmd/main.go` | 146 | Entry point, orchestration |
| `generator.go` | 64 | Generator struct, runForRoot |
| `schema.go` | 421 | Core schema building (buildRootSchema, schemaForExpr, collectReachable) |
| `marker.go` | 337 | Marker extraction and application |
| `const.go` | 123 | Const enum handling |
| `extractor.go` | 130 | Doc/comment extraction |
| `primitives.go` | 93 | Go primitive → JSON Schema type mapping |
| `tag.go` | 54 | JSON struct tag parsing |
| `builtin.go` | 41 | Runtime type built-in schemas |
| `writer/writer.go` | 27 | JSON schema file writer |
| `writer/embed.go` | 70 | Go embed file generator |
| `universe/build.go` | 550 | Package loading, type discovery, resolution |

---

## Summary of Expected Gains

| Improvement | Cold Cache | Warm Cache | Complexity |
|---|---|---|---|
| 1. Pre-build binary | **-16s** | -0.3s | Trivial |
| 2. Targeted package loading | **-3-4s** | -0.2s | Medium |
| 3. WalkDir for marker scan | -0.1s | -0.07s | Easy |
| 4. CI cache improvement | -5-10s | N/A | Easy |
| ~~5. Parallel generators~~ | ~~N/A~~ | ~~N/A~~ | **NOT SAFE** (race on shared dirs) |
| **Combined (1-4)** | **~20s → <2s** | **~6.5s → <0.3s** | |
