# jsonschemagen Performance Optimization

* **Status**: accepted
* **Deciders**: jakobmoellerdev
* **Date**: 2026-04-13

Technical Story: `jsonschemagen` generate task dominated CI wall time due to `go run`
recompilation and loading 6.8x more packages than necessary.

## Context and Problem Statement

The `jsonschemagen` code generator produces JSON Schema Draft 2020-12 definitions from Go
types annotated with `+ocm:jsonschema-gen` markers. It runs as part of `task generate` in CI
on every PR to verify generated code is up to date.

Profiling revealed that **actual schema generation accounts for <4% of total runtime**. The
remaining >96% is Go toolchain overhead: recompiling the tool binary and type-checking
packages that contain no annotated types.

## Decision Drivers

* CI generate job wall time directly impacts PR feedback latency
* 85% of packages loaded by `packages.Load` contain zero annotated types
* Sibling generator `ocmtypegen` already uses pre-built binary pattern
* Generator parallelization is not safe (27/28 target dirs shared between generators)

## Considered Options

* Option 1: Pre-build binary + targeted package loading + filesystem marker discovery
* Option 2: Incremental/cached generation (skip unchanged types)
* Option 3: Parallelize `ocmtypegen` and `jsonschemagen`

## Decision Outcome

Chosen [Option 1](#option-1): "Pre-build binary + targeted package loading + filesystem marker
discovery".

Justification:

* Eliminates recompilation overhead entirely (measured ~54x speedup cold, ~2.6x warm)
* Reduces packages loaded from 190 to ~30 (6.3x reduction)
* No functional changes — output byte-identical across all 81 annotated types
* Low risk — each fix is independently reversible

### Option 1

#### Description

Three complementary fixes applied to the generate pipeline:

**Fix A — Pre-build binary**: Changed `Taskfile.yml` from `go run` to
`go build -o` + run binary, matching `ocmtypegen` pattern. Eliminates
per-invocation compilation of `golang.org/x/tools/go/packages` and transitive deps.

**Fix B — Targeted package loading**: Changed `packages.Load(cfg, "./...")`
(loads every package in a module) to `packages.Load(cfg, "./specific/pkg", ...)`
(loads only packages containing markers). Added `Patterns []string` field to
`LoadTarget` struct.

**Fix C — Filesystem marker discovery**: Replaced `packages.Load(NeedFiles)` per
module (spawns `go list` subprocess) with `filepath.WalkDir` scanning `.go` files
directly. Skips vendor, testdata, hidden dirs, test files, generated files to match
Go tool conventions.

#### Measured Results (Multiples, Not Absolute Times)

All measurements as multiples of optimized binary runtime (baseline = 1.0x):

| Scenario | Before | After | Speedup |
|---|---|---|---|
| Binary runtime (warm cache) | 2.6x | **1.0x** | 2.6x |
| `go run` (warm cache) | 4.4x | **1.0x** | 4.4x |
| `go run` (cold cache / CI) | 54x | **1.0x** | ~54x |

| Metric | Before | After | Reduction |
|---|---|---|---|
| Packages loaded | 190 | 30 | 6.3x fewer |
| Types in universe | 554 | 150 | 3.7x fewer |
| Annotated types found | 81 | 81 | Identical |
| Schema output | Baseline | Byte-identical | No change |

#### Package Loading Waste (Before Fix)

When a module had even one annotated file, ALL packages in that module were loaded
with full type-checking. Worst offenders by waste ratio:

| Module | Loaded | Annotated | Ratio |
|---|---|---|---|
| cli | 55 | 1 | 55x over |
| plugin | 34 | 1 | 34x over |
| oci | 37 | 5 | 7.4x over |
| helm | 12 | 3 | 4x over |
| blob | 10 | 1 | 10x over |
| transform | 9 | 1 | 9x over |
| **Total** | **190** | **28** | **6.8x over** |

#### Architecture (Before vs After)

Before:

```text
findModuleRoots (28 modules)
  → packages.Load(NeedFiles) per module (28 go list subprocesses)
  → filter to 15 modules with markers
  → packages.Load(NeedSyntax|NeedTypes|..., "./...") per module
  → 190 packages type-checked → 554 types → filter to 81 annotated
```

After:

```text
findModuleRoots (28 modules)
  → filepath.WalkDir per module (pure filesystem, no subprocesses)
  → collect specific package dirs with markers
  → packages.Load(NeedSyntax|NeedTypes|..., "./specific/pkg") per module
  → 30 packages type-checked → 150 types → filter to 81 annotated
```

#### Contract

Public API unchanged. `universe.Build()` signature identical. `LoadTarget` struct
gains one field:

```go
type LoadTarget struct {
    Type     string   // unchanged
    Path     string   // unchanged
    Patterns []string // NEW: specific package patterns (nil = "./...")
    Required bool     // unchanged
}
```

Callers passing `Patterns: nil` get previous behavior (`"./..."`).

## Pros and Cons of the Options

### [Option 1] Pre-build binary + targeted loading + filesystem discovery

Pros:

* ~54x cold-cache speedup, ~2.6x warm-cache speedup
* Output byte-identical — zero functional risk
* Each fix independently reversible
* Matches existing `ocmtypegen` patterns

Cons:

* `Patterns` field adds minor complexity to `LoadTarget`
* Filesystem walking doesn't respect Go build constraints (mitigated: Phase 2
  loading still respects them via `packages.Load`)

### [Option 2] Incremental/cached generation

Pros:

* Would skip unchanged types entirely
* Near-zero runtime for no-op runs

Cons:

* Requires cache invalidation logic (file timestamps, content hashes)
* Complex to implement correctly — must track transitive type dependencies
* CI runs on clean checkout, so cache would need artifact storage
* Diminishing returns after Option 1 (baseline already fast)

### [Option 3] Parallelize generators

Pros:

* Would overlap `ocmtypegen` and `jsonschemagen` runtime

Cons:

* **NOT SAFE**: 27/28 target directories shared between generators
* `packages.Load(NeedSyntax)` reads ALL `.go` files including
  `zz_generated.ocm_type.go` — race condition with concurrent `ocmtypegen` writes
* Sequential ordering in `Taskfile.yml` must be preserved

## Discovery and Distribution

Changes shipped in three files:

* `bindings/go/generator/Taskfile.yml` — build step + binary invocation
* `bindings/go/generator/universe/build.go` — WalkDir discovery + targeted patterns
* All existing tests pass, generation output verified identical

## Conclusion

Three targeted fixes reduced `jsonschemagen` overhead by eliminating recompilation
(~54x cold speedup) and reducing package loading scope (6.3x fewer packages). Schema
output unchanged across all 81 annotated types. Generator parallelization rejected as
unsafe due to shared target directories.
