#!/usr/bin/env bash
set -euo pipefail

if [ $# -ne 2 ]; then
  echo "usage: prepare-scaffolding.sh VERSION SETUP_DIR" >&2
  exit 1
fi

VERSION="${1}"
SETUP_DIR="${2}"

# Step 1: validate VERSION format
if ! printf '%s\n' "${VERSION}" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$'; then
  echo "sigstore setup: invalid version: ${VERSION}" >&2
  exit 1
fi

# Step 2: source the shared download helpers
HELPERS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./setup-helpers.sh
. "${HELPERS_DIR}/setup-helpers.sh"

# Step 3: create a staging directory; remove it on exit unless promoted
mkdir -p "${SETUP_DIR}"
stage="$(mktemp -d "${SETUP_DIR}/stage.XXXXXX")"
promoted=false
cleanup() {
  if [ "${promoted}" != "true" ]; then
    rm -rf "${stage}"
  fi
}
trap cleanup EXIT

# Step 4: base release URL
BASE_URL="https://github.com/sigstore/scaffolding/releases/download/${VERSION}"

# Step 5: required assets: any failure aborts
for asset in setup-kind.sh setup-scaffolding-from-release.sh testrelease.yaml \
             release-fulcio.yaml release-ctlog.yaml release-tuf.yaml release-tsa.yaml; do
  rc=0
  download_file "${BASE_URL}/${asset}" "${stage}/${asset}" || rc=$?
  if [ "${rc}" -ne 0 ]; then
    echo "sigstore setup: required asset download failed: ${asset}" >&2
    exit 1
  fi
done

# Step 6: optional assets: 404 is expected for some releases, other failures abort
for asset in release-trillian.yaml release-rekor.yaml release-rekor-tiles.yaml; do
  rc=0
  download_file "${BASE_URL}/${asset}" "${stage}/${asset}" || rc=$?
  if [ "${rc}" -eq 4 ]; then
    echo "sigstore setup: optional asset absent: ${asset}"
  elif [ "${rc}" -ne 0 ]; then
    echo "sigstore setup: optional asset download failed: ${asset}" >&2
    exit 1
  fi
done

# Step 7: at least one Rekor variant must be present
if [ ! -f "${stage}/release-rekor.yaml" ] && [ ! -f "${stage}/release-rekor-tiles.yaml" ]; then
  echo "sigstore setup: unsupported release assets" >&2
  exit 1
fi

# Step 8: installer contract: verify --release-dir support and known YAML references
installer="${stage}/setup-scaffolding-from-release.sh"
if [ "$(grep -c -- '--release-dir)' "${installer}")" -lt 1 ]; then
  echo "sigstore setup: unsupported scaffolding script layout" >&2
  exit 1
fi
# Every ${RELEASE_BASE}/release-*.yaml the installer names must be in our known set.
installer_yamls="$(grep -oE '\$\{RELEASE_BASE\}/release-[A-Za-z0-9._-]+\.yaml' "${installer}" || true)"
for ref in ${installer_yamls}; do
  name="${ref##*/}"
  case "${name}" in
    release-fulcio.yaml|release-ctlog.yaml|release-tuf.yaml|release-tsa.yaml|\
    release-trillian.yaml|release-rekor.yaml|release-rekor-tiles.yaml) ;;
    *)
      echo "sigstore setup: unsupported scaffolding script layout" >&2
      exit 1
      ;;
  esac
done

# Step 9: patch staged setup-kind.sh (all-or-nothing)
kind_sh="${stage}/setup-kind.sh"

# Verify all anchor counts before applying any patch
cnt_pipefail="$(grep -c '^set -o pipefail$' "${kind_sh}" || true)"
cnt_metallb="$(grep -cE '^kubectl apply -f https://raw\.githubusercontent\.com/metallb/metallb/[^ ]+$' "${kind_sh}" || true)"
cnt_rb="$(grep -c '^function resource_blaster() {$' "${kind_sh}" || true)"
if [ "${cnt_pipefail}" -ne 1 ] || [ "${cnt_metallb}" -ne 1 ] || [ "${cnt_rb}" -ne 1 ]; then
  echo "sigstore setup: unsupported kind script layout" >&2
  exit 1
fi

# Patch a: source helpers and validate downloads env var immediately after set -o pipefail
ptmp="$(mktemp "${stage}/.patch.XXXXXX")"
awk '
/^set -o pipefail$/ {
  print
  print ". \"${SIGSTORE_SETUP_HELPERS:?}\""
  print ": \"${SIGSTORE_SETUP_DOWNLOADS:?}\""
  next
}
{ print }
' "${kind_sh}" > "${ptmp}" && mv "${ptmp}" "${kind_sh}"

# Patch b: config-network webhook retry: the Knative webhook rejects the patch if it
# is not yet ready, so we wrap the call in a while loop identical to the later
# config-features/-defaults/-autoscaler patches. Expressions verbatim from prior Taskfile.
ptmp="$(mktemp "${stage}/.patch.XXXXXX")"
sed \
  -e 's@^kubectl patch configmap/config-network@while ! kubectl patch configmap/config-network@' \
  -e 's@\(ingress\.class":"kourier.*\)$@\1; do echo Waiting for webhook to be up.; sleep 2; done@' \
  "${kind_sh}" > "${ptmp}" && mv "${ptmp}" "${kind_sh}"
grep -q '^while ! kubectl patch configmap/config-network' "${kind_sh}" && \
  grep -q 'ingress\.class.*; do echo Waiting for webhook' "${kind_sh}" || \
  { echo "sigstore setup: unsupported kind script layout" >&2; exit 1; }

# Patch c: MetalLB manifest. Pre-download so a transient GitHub fetch failure aborts here
# instead of letting kubectl apply an empty or truncated manifest.
ptmp="$(mktemp "${stage}/.patch.XXXXXX")"
awk '
/^kubectl apply -f https:\/\/raw\.githubusercontent\.com\/metallb\/metallb\// {
  url = $4
  print "download_file \"" url "\" \"${SIGSTORE_SETUP_DOWNLOADS}/metallb-native.yaml\""
  print "kubectl apply -f \"${SIGSTORE_SETUP_DOWNLOADS}/metallb-native.yaml\""
  next
}
{ print }
' "${kind_sh}" > "${ptmp}" && mv "${ptmp}" "${kind_sh}"

# Patch d: resource_blaster: replace the curl-and-pipe with download-then-render so that
# a failed or empty fetch stops the pipeline rather than piping HTML into kubectl.
rb_awk="$(mktemp "${stage}/.rb_awk.XXXXXX")"
cat > "${rb_awk}" << 'RB_AWK'
BEGIN { in_rb = 0 }
/^function resource_blaster\(\) \{$/ {
  in_rb = 1
  print "# OCM override: download to a file with retries before rendering, so a failed fetch"
  print "# stops the pipeline instead of feeding kubectl an empty or HTML manifest."
  print "function resource_blaster() {"
  print "  local REPO=\"${1}\""
  print "  local FILE=\"${2}\""
  print "  if [[ \"${KNATIVE_VERSION}\" == \"latest\" ]]; then"
  print "    echo \"sigstore setup: KNATIVE_VERSION=latest is not supported\" >&2"
  print "    return 1"
  print "  fi"
  print "  local url=\"https://github.com/knative/${REPO}/releases/download/knative-v${KNATIVE_VERSION}/${FILE}\""
  print "  local dest=\"${SIGSTORE_SETUP_DOWNLOADS}/${REPO}/${FILE}\""
  print "  download_file \"${url}\" \"${dest}\" || return 1"
  print "  yq e 'del(.spec.template.spec.containers[]?.resources)' \"${dest}\" | grep -v '^{}$'"
  print "}"
  next
}
in_rb { if (/^\}$/) { in_rb = 0 }; next }
{ print }
RB_AWK
ptmp="$(mktemp "${stage}/.patch.XXXXXX")"
awk -f "${rb_awk}" "${kind_sh}" > "${ptmp}" && mv "${ptmp}" "${kind_sh}"
rm -f "${rb_awk}"

# Patch e: post-checks: confirm no direct https apply lines remain and no curl-pipe remains
if grep -q 'kubectl apply -f https://' "${kind_sh}"; then
  echo "sigstore setup: unsupported kind script layout" >&2
  exit 1
fi
if grep -qF 'curl -L -s "${url}"' "${kind_sh}"; then
  echo "sigstore setup: unsupported kind script layout" >&2
  exit 1
fi
if ! bash -n "${kind_sh}"; then
  echo "sigstore setup: unsupported kind script layout" >&2
  exit 1
fi
if ! bash -n "${stage}/setup-scaffolding-from-release.sh"; then
  echo "sigstore setup: unsupported kind script layout" >&2
  exit 1
fi

# Step 10: promote stage to SETUP_DIR/release (atomic from caller's perspective)
rm -rf "${SETUP_DIR}/release"
mv "${stage}" "${SETUP_DIR}/release"
promoted=true
printf 'sigstore setup: staged scaffolding %s\n' "${VERSION}"
