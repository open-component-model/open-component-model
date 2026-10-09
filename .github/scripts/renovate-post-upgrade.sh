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

# Renovate drops gomodTidy on mixed-manager grouped branches, so tidy the root
# module here to avoid orphan go.sum entries.
if [ -f bindings/go/go.mod ]; then
  echo "Running explicit go mod tidy for bindings/go"
  (
    cd bindings/go
    go mod tidy
  )
fi