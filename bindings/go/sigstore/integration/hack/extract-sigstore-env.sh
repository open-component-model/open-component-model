#!/usr/bin/env bash
set -euo pipefail

# Extract sigstore verification material from a running scaffolding cluster
# and output env vars for the integration test suite.
#
# Prerequisites: kubectl, cosign, curl must be on PATH.
# The current kubectl context must point to the scaffolding cluster.

require_nonempty() {
  local name="$1" val="$2"
  if [[ -z "$val" ]]; then
    echo "extract-sigstore-env: $name is empty (scaffolding cluster not ready?)" >&2
    exit 1
  fi
}

# When SIGSTORE_BRIDGE_PORT is set (macOS port-forwarding), rewrite a Knative
# URL to include the port so traffic routes through kubectl port-forward.
# The hostname is preserved for Knative Host-header routing.
bridge_url() {
  local url="$1"
  if [[ -n "${SIGSTORE_BRIDGE_PORT:-}" ]]; then
    echo "$url" | sed "s|^\(http://[^/]*\)|\1:${SIGSTORE_BRIDGE_PORT}|"
  else
    echo "$url"
  fi
}

WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

FULCIO_URL=$(kubectl -n fulcio-system get ksvc fulcio -ojsonpath='{.status.url}')
REKOR_URL=$(kubectl -n rekor-system get ksvc rekor -ojsonpath='{.status.url}')
TSA_URL=$(kubectl -n tsa-system get ksvc tsa -ojsonpath='{.status.url}')
CTLOG_URL=$(kubectl -n ctlog-system get ksvc ctlog -ojsonpath='{.status.url}')
require_nonempty FULCIO_URL "$FULCIO_URL"
require_nonempty REKOR_URL  "$REKOR_URL"
require_nonempty TSA_URL    "$TSA_URL"
require_nonempty CTLOG_URL  "$CTLOG_URL"

# Extract public keys and certificates.
# Fulcio: fetch the CA cert from the running Fulcio API, NOT the K8s secret.
# The scaffolding's testrelease.yaml rotates the fulcio-pub-key secret but the
# original Fulcio instance keeps signing with its original CA cert.  The API
# endpoint always returns the cert that the running Fulcio actually uses.
curl -sSf "$(bridge_url "$FULCIO_URL")/api/v1/rootCert" > "$WORK_DIR/fulcio-root.pem"
require_nonempty "fulcio-root.pem" "$(cat "$WORK_DIR/fulcio-root.pem")"
# Rekor public key for `cosign trusted-root create` below. Rekor v2 (tile-based
# TesseraCT) has no v1 HTTP API to query, so read it from the rekor-pub-key secret,
# where the scaffolding setup injects the generated public key. The tlog entry cosign
# builds from this key is replaced further down (see the TUF splice) because cosign
# derives the wrong logId for a tiles log; this read just satisfies the create call.
kubectl -n rekor-system get secret rekor-pub-key -ojsonpath='{.data.public}' | base64 -d > "$WORK_DIR/rekor.pub"
require_nonempty "rekor.pub" "$(cat "$WORK_DIR/rekor.pub")"
kubectl -n ctlog-system get secret ctlog-public-key -ojsonpath='{.data.public}' | base64 -d > "$WORK_DIR/ctlog.pub"
require_nonempty "ctlog.pub" "$(cat "$WORK_DIR/ctlog.pub")"
kubectl -n tsa-system get secret tsa-cert-chain -ojsonpath='{.data.cert-chain}' | base64 -d > "$WORK_DIR/tsa-chain.pem"
require_nonempty "tsa-chain.pem" "$(cat "$WORK_DIR/tsa-chain.pem")"

# Timestamp shared by trusted root and signing config entries.
NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Build trusted root containing only local cluster material.
cosign trusted-root create \
  --no-default-fulcio \
  --no-default-rekor \
  --no-default-ctfe \
  --no-default-tsa \
  --fulcio="url=$(bridge_url "$FULCIO_URL"),certificate-chain=${WORK_DIR}/fulcio-root.pem,start-time=${NOW}" \
  --rekor="url=$(bridge_url "$REKOR_URL"),public-key=${WORK_DIR}/rekor.pub,start-time=${NOW}" \
  --ctfe="url=$(bridge_url "$CTLOG_URL"),public-key=${WORK_DIR}/ctlog.pub,start-time=${NOW}" \
  --tsa="url=$(bridge_url "$TSA_URL")/api/v1/timestamp,certificate-chain=${WORK_DIR}/tsa-chain.pem,start-time=${NOW}" \
  --out "${WORK_DIR}/trusted_root.json"

# Build signing config pointing at local cluster services
cosign signing-config create \
  --fulcio="url=$(bridge_url "$FULCIO_URL"),api-version=1,start-time=${NOW},operator=scaffolding" \
  --rekor="url=$(bridge_url "$REKOR_URL"),api-version=2,start-time=${NOW},operator=scaffolding" \
  --rekor-config=ANY \
  --tsa="url=$(bridge_url "$TSA_URL")/api/v1/timestamp,api-version=1,start-time=${NOW},operator=scaffolding" \
  --tsa-config=ANY \
  --out "${WORK_DIR}/signing_config.json"

# Initialize cosign's local TUF cache with the scaffolding's TUF mirror.
# The handler forwards the full parent process environment to the cosign
# subprocess, but cosign's TUF client uses ~/.sigstore/root/ as its cache
# regardless of env vars.  Pre-seeding it ensures cosign trusts the local
# scaffolding CA chain without network access to the public TUF mirror.
TUF_MIRROR=$(kubectl -n tuf-system get ksvc tuf -ojsonpath='{.status.url}')
require_nonempty TUF_MIRROR "$TUF_MIRROR"
# Compute the mirror URL once so `cosign initialize` and the cache-path
# derivation below refer to the exact same mirror.
TUF_MIRROR_URL=$(bridge_url "$TUF_MIRROR")
kubectl -n tuf-system get secrets tuf-root -ojsonpath='{.data.root}' | base64 -d > "$WORK_DIR/tuf-root.json"
require_nonempty "tuf-root.json" "$(cat "$WORK_DIR/tuf-root.json")"
cosign initialize --mirror "$TUF_MIRROR_URL" --root "$WORK_DIR/tuf-root.json" >&2

# Fix the Rekor tlog entry in our hand-built trusted root.
#
# `cosign trusted-root create` sets the tlog logId to SHA256(public key), which is wrong
# for a tiles log: it is identified by its checkpoint note keyhash instead. With the wrong
# logId, cosign finds no trusted log matching the bundle's checkpoint and verification fails
# with "not enough verified log entries" -- the key is right, only the logId is off.
#
# The scaffolding's TUF mirror publishes a trusted_root.json with the correct logId, so copy
# its tlogs entry over ours (Fulcio, CTLog and TSA stay as they are).
#
# cosign caches the mirror under ~/.sigstore/root/<dir>/targets/, where <dir> is the mirror
# URL transformed by sigstore-go's URLToPath(): drop the scheme, replace "/" and ":" with
# "-", then lowercase. We reproduce that transform so we read trusted_root.json from the
# exact mirror we just initialized -- not whatever unrelated ~/.sigstore/root entry (e.g. a
# cached public-good root, or a stale run with a different bridge port) happens to sort first.
# Ref: https://github.com/sigstore/sigstore-go/blob/main/pkg/tuf/client.go (URLToPath)
mirror_cache_dirname() {
  local url="$1"
  url="${url#http://}"          # strip scheme
  url="${url#https://}"
  url="${url//\//-}"            # "/" -> "-"
  url="${url//:/-}"             # ":" -> "-"
  printf '%s' "$url" | tr '[:upper:]' '[:lower:]'
}
TUF_TRUSTED_ROOT="${HOME}/.sigstore/root/$(mirror_cache_dirname "$TUF_MIRROR_URL")/targets/trusted_root.json"
if [[ ! -f "$TUF_TRUSTED_ROOT" ]]; then
  echo "extract-sigstore-env: cosign TUF cache missing at $TUF_TRUSTED_ROOT" >&2
  echo "  (did 'cosign initialize --mirror $TUF_MIRROR_URL' run against this mirror?)" >&2
  exit 1
fi
jq --slurpfile tuf "${TUF_TRUSTED_ROOT}" '.tlogs = $tuf[0].tlogs' \
  "${WORK_DIR}/trusted_root.json" > "${WORK_DIR}/trusted_root.spliced.json"
mv "${WORK_DIR}/trusted_root.spliced.json" "${WORK_DIR}/trusted_root.json"

# Copy artifacts to a stable location (not cleaned up by trap)
OUTDIR="${SIGSTORE_ENV_DIR:-$(pwd)/tmp/sigstore}"
mkdir -p "$OUTDIR"
cp "$WORK_DIR/trusted_root.json" "$OUTDIR/"
cp "$WORK_DIR/signing_config.json" "$OUTDIR/"

# Fetch OIDC token
ISSUER_URL=$(kubectl -n default get ksvc gettoken -ojsonpath='{.status.url}')
require_nonempty ISSUER_URL "$ISSUER_URL"
OIDC_TOKEN=$(curl -sSf "$(bridge_url "$ISSUER_URL")")
require_nonempty OIDC_TOKEN "$OIDC_TOKEN"

# Output env vars (sourceable) — use printf %q to safely escape values
emit_export() {
  local name="$1" val="$2"
  printf 'export %s=%q\n' "$name" "$val"
}

# The FULCIO/REKOR/TSA URL exports below are for developer convenience (e.g.
# manual cosign invocations) -- the integration tests do not consume them.
emit_export SIGSTORE_FULCIO_URL    "$(bridge_url "$FULCIO_URL")"
emit_export SIGSTORE_REKOR_URL     "$(bridge_url "$REKOR_URL")"
emit_export SIGSTORE_TSA_URL       "$(bridge_url "$TSA_URL")"
emit_export SIGSTORE_OIDC_TOKEN    "$OIDC_TOKEN"
emit_export SIGSTORE_TRUSTED_ROOT  "$OUTDIR/trusted_root.json"
emit_export SIGSTORE_SIGNING_CONFIG "$OUTDIR/signing_config.json"
emit_export SIGSTORE_OIDC_ISSUER   "https://kubernetes.default.svc.cluster.local"
emit_export SIGSTORE_OIDC_IDENTITY "https://kubernetes.io/namespaces/default/serviceaccounts/default"
