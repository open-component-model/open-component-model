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

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# Service URLs from Knative services
FULCIO_URL=$(kubectl -n fulcio-system get ksvc fulcio -ojsonpath='{.status.url}')
REKOR_URL=$(kubectl -n rekor-system get ksvc rekor -ojsonpath='{.status.url}')
TSA_URL=$(kubectl -n tsa-system get ksvc tsa -ojsonpath='{.status.url}')
CTLOG_URL=$(kubectl -n ctlog-system get ksvc ctlog -ojsonpath='{.status.url}')
require_nonempty FULCIO_URL "$FULCIO_URL"
require_nonempty REKOR_URL  "$REKOR_URL"
require_nonempty TSA_URL    "$TSA_URL"
require_nonempty CTLOG_URL  "$CTLOG_URL"

# Extract public keys and certificates from cluster secrets
kubectl -n fulcio-system get secret fulcio-pub-key -ojsonpath='{.data.cert}' | base64 -d > "$TMPDIR/fulcio-root.pem"
kubectl -n rekor-system get secret rekor-pub-key -ojsonpath='{.data.public}' | base64 -d > "$TMPDIR/rekor.pub"
kubectl -n ctlog-system get secret ctlog-public-key -ojsonpath='{.data.public}' | base64 -d > "$TMPDIR/ctlog.pub"
kubectl -n tsa-system get secret tsa-cert-chain -ojsonpath='{.data.cert-chain}' | base64 -d > "$TMPDIR/tsa-chain.pem"

NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Build trusted root containing only local cluster material
cosign trusted-root create \
  --fulcio="url=${FULCIO_URL},certificate-chain=${TMPDIR}/fulcio-root.pem,start-time=${NOW}" \
  --rekor="url=${REKOR_URL},public-key=${TMPDIR}/rekor.pub,start-time=${NOW}" \
  --ctfe="url=${CTLOG_URL},public-key=${TMPDIR}/ctlog.pub,start-time=${NOW}" \
  --tsa="url=${TSA_URL}/api/v1/timestamp,certificate-chain=${TMPDIR}/tsa-chain.pem,start-time=${NOW}" \
  --no-default-fulcio --no-default-rekor --no-default-ctfe --no-default-tsa \
  --out "${TMPDIR}/trusted_root.json"

# Build signing config pointing at local cluster services
cosign signing-config create \
  --fulcio="url=${FULCIO_URL},api-version=1,start-time=${NOW},operator=scaffolding" \
  --rekor="url=${REKOR_URL},api-version=2,start-time=${NOW},operator=scaffolding" \
  --rekor-config=ANY \
  --tsa="url=${TSA_URL}/api/v1/timestamp,api-version=1,start-time=${NOW},operator=scaffolding" \
  --tsa-config=ANY \
  --out "${TMPDIR}/signing_config.json"

# Copy artifacts to a stable location (not cleaned up by trap)
OUTDIR="${SIGSTORE_ENV_DIR:-$(pwd)/tmp/sigstore}"
mkdir -p "$OUTDIR"
cp "$TMPDIR/trusted_root.json" "$OUTDIR/"
cp "$TMPDIR/signing_config.json" "$OUTDIR/"

# Fetch OIDC token
ISSUER_URL=$(kubectl -n default get ksvc gettoken -ojsonpath='{.status.url}')
require_nonempty ISSUER_URL "$ISSUER_URL"
OIDC_TOKEN=$(curl -sSf "$ISSUER_URL")
require_nonempty OIDC_TOKEN "$OIDC_TOKEN"

# Output env vars (sourceable) — use printf %q to safely escape values
emit_export() {
  local name="$1" val="$2"
  printf 'export %s=%q\n' "$name" "$val"
}

emit_export SIGSTORE_FULCIO_URL    "$FULCIO_URL"
emit_export SIGSTORE_REKOR_URL     "$REKOR_URL"
emit_export SIGSTORE_TSA_URL       "$TSA_URL"
emit_export SIGSTORE_OIDC_TOKEN    "$OIDC_TOKEN"
emit_export SIGSTORE_TRUSTED_ROOT  "$OUTDIR/trusted_root.json"
emit_export SIGSTORE_SIGNING_CONFIG "$OUTDIR/signing_config.json"
emit_export SIGSTORE_OIDC_ISSUER   "https://kubernetes.default.svc.cluster.local"
emit_export SIGSTORE_OIDC_IDENTITY "https://kubernetes.io/namespaces/default/serviceaccounts/default"
