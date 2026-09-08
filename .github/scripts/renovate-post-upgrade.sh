#!/usr/bin/env sh
set -e

# Exit early if Go is not available
if ! command -v go >/dev/null 2>&1; then
  exit 0
fi

# Find all go.mod files under */integration/* and run go mod tidy in their directories
find . -type f -name "go.mod" -path "*/integration/*" -exec dirname {} \; | while IFS= read -r dir; do
  echo "Running explicit go mod tidy for integration test in $dir"
  (
    cd "$dir"
    go mod tidy
  )
done

# Fix Go major-version bumps that Renovate leaves half-applied.
# Renovate updates the /vN require line in go.mod but cannot rewrite import
# paths in .go source (see #3568). gomajor does both; run it against any
# module whose /vN path changed in this branch, pinned to the exact version
# Renovate chose so we do not drift to a newer major.
#
# gomajor is installed into $HOME/go/bin here rather than /usr/local/bin
# because the Renovate container runs as an unprivileged user. It is
# invoked via its absolute path so PATH does not need to be modified.
GOBIN="$(go env GOPATH)/bin"
export GOBIN
# renovate: datasource=go depName=github.com/icholy/gomajor
GOMAJOR_VERSION=v0.15.0
if ! "${GOBIN}/gomajor" version 2>/dev/null | grep -qx "version: ${GOMAJOR_VERSION}"; then
  go install "github.com/icholy/gomajor@${GOMAJOR_VERSION}"
fi

base_ref="${RENOVATE_BASE_BRANCH:-origin/main}"
if ! git rev-parse --verify "${base_ref}" >/dev/null 2>&1; then
  echo "warning: base ref '${base_ref}' does not resolve; falling back to HEAD~1" >&2
  base_ref="HEAD~1"
fi

for gomod in $(git diff --name-only "${base_ref}" -- '**/go.mod'); do
  dir=$(dirname "${gomod}")
  echo "Checking ${gomod} for Go major-version bumps"
  # Emit lines like: +<indent>github.com/foo/bar/v3 v3.0.1 for added requires.
  git diff "${base_ref}" -- "${gomod}" \
    | grep -E '^\+[[:space:]]+[^[:space:]]+/v[0-9]+ v[0-9]+' \
    | while read -r plus path version _rest; do
        (cd "${dir}" && "${GOBIN}/gomajor" get "${path}@${version}") || {
          echo "gomajor get ${path}@${version} failed in ${dir}, continuing" >&2
        }
      done
  (cd "${dir}" && go mod tidy)
done