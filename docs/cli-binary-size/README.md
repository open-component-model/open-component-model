# CLI binary size over one year

This folder holds a one-off measurement of the `ocm` CLI binary size across roughly the last year of CLI-touching commits. The data supports the size threshold used by the `cli binary size guard` workflow (`.github/workflows/cli-binary-size.yml`).

## Files

- `binary-size.csv` is the source of truth. It holds one row per weekly sample.
- `binary-size.svg` is a time-series graph rendered from the CSV.
- The measurement script is `bindings/go/cli/hack/measure-binary-size.sh`.

## Method

The script samples one CLI-touching commit per ISO week over the trailing 52 weeks. It picks the newest CLI-touching commit in each week. A commit counts as CLI-touching when it changes files under the CLI source tree.

The CLI source tree moved during the year. Commit `2d980e5624` moved the CLI out of the repository-root `cli/` module into `bindings/go/cli`. The script builds `cli/` for commits before that move and `bindings/go/cli` for commits at or after it. The CLI-touching filter follows the same path split.

The required Go version changed over the year, from 1.25.1 to 1.27.x. The script reads each sample go.mod and uses `asdf` to install and select a matching Go toolchain, so historical builds succeed. It prefers the `toolchain` directive when present, and falls back to the `go` directive.

Each sample is built for one fixed target, `GOOS=linux GOARCH=amd64`, with the release ldflags `-s -w`. The script records the stripped binary size in bytes. The build flags are identical across all samples, so the samples are comparable.

## Results

The 52 weekly samples were selected from 587 CLI-touching commits in the window. All 52 samples built. Five samples failed to build on the first pass because of transient module-download failures, and all five built on retry, so the final data set has 52 built samples and 0 skipped.

The binary grew from about 11.6 MiB in week 2025-W48 to about 50.9 MiB in week 2026-W47. The largest sample was about 59.9 MiB in week 2026-W38. Most of the year-over-year growth comes from new features and from Go toolchain changes, not from steady per-commit drift.

Week-over-week change distribution:

- All consecutive weeks: median about 0.2 percent, 90th percentile about 3.4 percent, 95th percentile about 41.7 percent.
- Consecutive weeks with the same Go toolchain: median about 0.1 percent, 90th percentile about 3.2 percent, 95th percentile about 8.1 percent.

The same-toolchain distribution matches the conditions of the CI guard, because the guard builds the new commit and its parent with one toolchain. The large outliers in the all-weeks distribution line up with Go toolchain changes between weeks, which the guard removes by design.

A few same-toolchain weeks still show large jumps, for example about 8 percent, 42 percent, and 96 percent. Each weekly step bundles every CLI change that landed that week, so these steps overstate the growth of a single change. The guard measures one push, which is a subset of a week.

## Recommended threshold

Use a default threshold of 10 percent.

The value sits above the 95th percentile of same-toolchain week-over-week growth, which is about 8 percent. Normal feature work stays below it. Because a single push is a subset of a week, a single push that grows the binary by more than 10 percent is very likely an anomaly. The guard fails in that case and asks a maintainer to judge whether the growth is justified. A maintainer can raise the threshold for one run through the `workflow_dispatch` input when a large increase is expected and approved.

## Notable moves and their causes

The guard compares a commit with its parent using one toolchain, so only same-toolchain moves are relevant to it. The large moves in the data break down as follows.

- 2026-W07, about +96 percent (about 18.0 to 35.3 MiB), same toolchain. Commit #1756 registered helm as a built-in input plugin. It added `helm.sh/helm/v4` and the full Kubernetes client tree (`k8s.io/client-go`, `k8s.io/kubectl`, `sigs.k8s.io/controller-runtime`, `sigs.k8s.io/kustomize`) to the CLI. This is a sustained increase and is the clearest example of a heavy dependency entering the CLI. The guard fails on a change like this and asks for review.
- 2026-W38, about +42 percent (about 42.3 to 59.9 MiB), same toolchain, followed by a return the next week. A per-commit binary search pinned the cause to commit #3618, which added a human-readable version output and set a cobra version template (`SetVersionTemplate` with `AddTemplateFunc`). The template path defeated the linker dead-code elimination and kept about 17 MiB of otherwise-pruned code. No dependency changed. Commit #3693 then dropped the cobra version template and restored dead-code elimination, which removed the 17 MiB again. Building the bloated commit with the newer Go toolchain did not remove the growth, so the fix was in code, not in the toolchain. This is the clearest example of growth with no dependency change, which is why the guard measures the linked binary and not the dependency list.
- 2026-W22, about -23 percent (about 49.3 to 38.1 MiB), same toolchain. This decrease lines up with the Kubernetes dependency major bump to v0.36 that week. The exact reason for the drop needs a bisect.
- 2025-W50, about +50 percent, and 2026-W11, about +36 percent. Both are Go toolchain upgrades (1.25.1 to 1.25.4, and 1.25.7 to 1.26.1). The guard excludes this class of move by design, because it builds both ends with one toolchain.

The one sustained same-toolchain jump above the threshold in the year is the helm plugin in W07, which is exactly the kind of change the guard should hold for review. The W38 spike is a second example: a version-output feature added about 17 MiB through a linker dead-code-elimination regression, with no dependency change, and a fix restored the size a week later. The toolchain driven jumps are filtered out by the build-both-ends design.

## Reproduce

Run the script from the repository root:

```bash
bindings/go/cli/hack/measure-binary-size.sh
```

The script needs `git`, `asdf` with the `golang` plugin, and a network connection to fetch historical module dependencies. It uses `gnuplot` to render the graph when `gnuplot` is present. The CSV is still written when `gnuplot` is absent.
