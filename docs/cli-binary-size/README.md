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

## Reproduce

Run the script from the repository root:

```bash
bindings/go/cli/hack/measure-binary-size.sh
```

The script needs `git`, `asdf` with the `golang` plugin, and a network connection to fetch historical module dependencies. It uses `gnuplot` to render the graph when `gnuplot` is present. The CSV is still written when `gnuplot` is absent.
