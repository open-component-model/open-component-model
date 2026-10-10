#!/usr/bin/env bash
# Prints `docker buildx build` --annotation arguments, one per line, carrying the
# pre-defined OCI annotations (https://github.com/opencontainers/image-spec/blob/main/annotations.md).
#
# Usage: oci-annotations.sh <containerfile> <stage> <version> <title> <description> [levels]
#   stage   final stage name, or "" for the last FROM. A "scratch" base yields no base.* annotations.
#   levels  buildx annotation levels (default "manifest,index"); single-platform
#           builds have no index, so pass "manifest" there.
set -euo pipefail

containerfile=$1 stage=$2 version=$3 title=$4 description=$5 levels=${6:-manifest,index}

# Base image of the target stage: "FROM [--platform=...] <image> [AS <stage>]".
base=$(awk -v stage="$stage" '
  toupper($1) == "FROM" {
    img = ""; name = ""
    for (i = 2; i <= NF; i++) {
      if ($i ~ /^--/) continue
      if (img == "") { img = $i; continue }
      if (toupper($i) == "AS") name = $(i + 1)
    }
    if (stage == "" || name == stage) found = img
  }
  END { print found }' "$containerfile")
[ -n "$base" ] || { echo "no FROM for stage '$stage' in $containerfile" >&2; exit 1; }

if [ -n "${SOURCE_DATE_EPOCH:-}" ]; then
  created=$(date -u -d "@$SOURCE_DATE_EPOCH" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -r "$SOURCE_DATE_EPOCH" +%Y-%m-%dT%H:%M:%SZ)
else
  created=$(date -u +%Y-%m-%dT%H:%M:%SZ)
fi

repo=https://github.com/open-component-model/open-component-model
revision=$(git rev-parse HEAD)
annotate() { printf -- '--annotation=%s:org.opencontainers.image.%s=%s\n' "$levels" "$1" "$2"; }

annotate created "$created"
annotate authors "Open Component Model Project"
annotate url "https://ocm.software"
annotate documentation "https://ocm.software/docs/"
annotate source "$repo"
annotate version "$version"
annotate revision "$revision"
annotate vendor "Open Component Model"
annotate licenses "Apache-2.0"
annotate title "$title"
annotate description "$description"
if [ "$base" != "scratch" ]; then
  annotate base.name "${base%@*}"
  [[ "$base" == *@sha256:* ]] && annotate base.digest "${base#*@}"
fi
