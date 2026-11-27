#!/usr/bin/env bash
#
# measure-binary-size.sh charts the stripped size of the `ocm` CLI binary
# across roughly the last year of CLI-touching commits. It samples one
# CLI-touching commit per ISO week, builds each sample for a single fixed
# target with the release ldflags, and records the byte size in a CSV. The
# CSV is the source of truth; a time-series graph is rendered from it when a
# plotting tool is available.
#
# HISTORY SPLIT: commit 2d980e5624 moved the CLI out of the repo-root `cli/`
# Go module into `bindings/go/cli`. For commits before that move the script
# builds `cli/`; at or after it builds `bindings/go/cli`. The CLI-touching
# filter follows the same path split.
#
# TOOLCHAIN DRIFT: the required Go version changed over the year. The script
# reads each sample's go.mod and uses asdf to install and select a matching
# Go toolchain, so historical builds succeed. Samples whose toolchain cannot
# be obtained, or that fail to build, are recorded as skipped and the run
# continues.
#
# Usage: bindings/go/cli/hack/measure-binary-size.sh [OUT_DIR]
#   OUT_DIR   output directory for the CSV and graph
#             (default: docs/cli-binary-size at the repo root)
#
# Environment overrides:
#   WEEKS         number of trailing weeks to sample (default 52)
#   GOOS GOARCH   fixed build target (default linux amd64)
#   SPLIT_COMMIT  the CLI move commit (default 2d980e5624...)

set -euo pipefail

WEEKS="${WEEKS:-52}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
SPLIT_COMMIT="${SPLIT_COMMIT:-2d980e56240a47820e36bd99f0d900c3ac36621b}"

REPO_ROOT="$(git rev-parse --show-toplevel)"
OUT_DIR="${1:-${REPO_ROOT}/docs/cli-binary-size}"
CSV="${OUT_DIR}/binary-size.csv"
SVG="${OUT_DIR}/binary-size.svg"

mkdir -p "${OUT_DIR}"

# file size in bytes, portable across GNU and BSD stat.
file_size() {
    stat -f%z "$1" 2>/dev/null || stat -c%s "$1"
}

if ! command -v asdf >/dev/null 2>&1; then
    echo "asdf is required to select historical Go toolchains" >&2
    exit 1
fi
if ! asdf plugin list 2>/dev/null | grep -qx golang; then
    asdf plugin add golang
fi

# Split boundary: build cli/ before this commit's date, bindings/go/cli at or
# after it.
SPLIT_EPOCH="$(git show -s --format='%ct' "${SPLIT_COMMIT}")"
SINCE="$(git log -1 --format='%ct' HEAD)"
SINCE="@$(( SINCE - WEEKS * 7 * 24 * 3600 ))"

echo "Building weekly samples from ${SINCE} (${WEEKS} weeks) for ${GOOS}/${GOARCH}" >&2

# One CLI-touching commit per ISO week, newest wins. Pre-split commits are
# matched on cli/, post-split on bindings/go/cli/. Columns: iso_week ct sha.
{
    git log --since="${SINCE}" --until="@${SPLIT_EPOCH}" \
        --format='%cd %ct %H' --date=format:'%G-W%V' -- cli/
    git log --since="@${SPLIT_EPOCH}" \
        --format='%cd %ct %H' --date=format:'%G-W%V' -- bindings/go/cli/
} | sort -k2,2nr | awk '!seen[$1]++' | sort -k1,1 > /tmp/ocm-size-samples.txt

SAMPLE_COUNT="$(wc -l < /tmp/ocm-size-samples.txt | tr -d ' ')"
echo "Selected ${SAMPLE_COUNT} weekly samples" >&2

# Each sample's tree is materialized with `git archive` into a scratch dir, so
# the run never checks out commits or mutates the current working tree. The
# CLI module was self-contained in both eras (no local replace directives), so
# the matching subtree is all that go build needs.
WORK="$(mktemp -d)"
cleanup() {
    rm -rf "${WORK}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "iso_week,commit_date,commit,cli_dir,go_version,status,size_bytes,note" > "${CSV}"

built=0
skipped=0

while read -r iso_week ct sha; do
    commit_date="$(git show -s --format='%cI' "${sha}")"

    if [[ "${ct}" -lt "${SPLIT_EPOCH}" ]]; then
        cli_dir="cli"
        archive_prefix="cli"
        version_pkg="ocm.software/open-component-model/cli/cmd/version"
    else
        cli_dir="bindings/go/cli"
        archive_prefix="bindings/go"
        version_pkg="ocm.software/open-component-model/bindings/go/cli/cmd/version"
    fi

    # Materialize the module subtree for this commit into a fresh scratch dir.
    tree="${WORK}/tree"
    rm -rf "${tree}"
    mkdir -p "${tree}"
    git archive "${sha}" "${archive_prefix}" | tar -x -C "${tree}"

    # go.mod lives at the module root: bindings/go post-split, cli pre-split.
    gomod="${tree}/${archive_prefix}/go.mod"
    build_dir="${tree}/${cli_dir}"

    # Effective toolchain: the `toolchain` directive if present, else the `go`
    # directive.
    go_version="$(awk '/^toolchain go/{print substr($2,3); f=1} END{if(!f) exit 1}' "${gomod}" 2>/dev/null || \
                  awk '/^go /{print $2; exit}' "${gomod}")"

    if ! asdf list golang 2>/dev/null | tr -d ' *' | grep -qx "${go_version}"; then
        if ! asdf install golang "${go_version}" >/dev/null 2>&1; then
            echo "SKIP ${iso_week} ${sha:0:12} go ${go_version}: toolchain unavailable" >&2
            echo "${iso_week},${commit_date},${sha},${cli_dir},${go_version},skipped,,toolchain-unavailable" >> "${CSV}"
            skipped=$(( skipped + 1 ))
            continue
        fi
    fi

    # asdf resolves the toolchain from a .tool-versions file in the build dir.
    echo "golang ${go_version}" > "${build_dir}/.tool-versions"
    out="${WORK}/ocm-${GOOS}-${GOARCH}"
    rm -f "${out}"
    if (cd "${build_dir}" && \
        CGO_ENABLED=0 GOTOOLCHAIN=local \
        GOOS="${GOOS}" GOARCH="${GOARCH}" \
        asdf exec go build \
            -ldflags "-s -w -X ${version_pkg}.BuildVersion=measure" \
            -o "${out}" ./ ) >/dev/null 2>&1; then
        size="$(file_size "${out}")"
        echo "OK   ${iso_week} ${sha:0:12} go ${go_version} ${cli_dir} ${size} bytes" >&2
        echo "${iso_week},${commit_date},${sha},${cli_dir},${go_version},built,${size}," >> "${CSV}"
        built=$(( built + 1 ))
    else
        echo "SKIP ${iso_week} ${sha:0:12} go ${go_version} ${cli_dir}: build failed" >&2
        echo "${iso_week},${commit_date},${sha},${cli_dir},${go_version},skipped,,build-failed" >> "${CSV}"
        skipped=$(( skipped + 1 ))
    fi
done < /tmp/ocm-size-samples.txt

echo >&2
echo "Built ${built}, skipped ${skipped} of ${SAMPLE_COUNT} weekly samples" >&2
echo "CSV: ${CSV}" >&2

# Render the time-series graph from the CSV. gnuplot is portable and present
# on the maintainer toolchains; if it is missing the CSV still stands alone.
if command -v gnuplot >/dev/null 2>&1; then
    gnuplot -e "
        set datafile separator ',';
        set terminal svg size 1000,520 font 'sans,12';
        set output '${SVG}';
        set title 'ocm CLI stripped binary size (${GOOS}/${GOARCH}), weekly samples';
        set xlabel 'ISO week';
        set ylabel 'size (MiB)';
        set grid ytics;
        set key off;
        set xtics rotate by -60 nomirror;
        set style line 1 lw 2 pt 7 ps 0.6 lc rgb '#1f77b4';
        plot '< grep built \"${CSV}\"' using 0:(\$7/1048576):xtic(1) with linespoints ls 1;
    " && echo "Graph: ${SVG}" >&2
else
    echo "gnuplot not found; skipped graph rendering (CSV is the source of truth)" >&2
fi
