#!/usr/bin/env bash
set -euo pipefail

# Extract sigstore verification material from a running scaffolding cluster
# and output env vars for the integration test suite.
#
# Prerequisites: kubectl, cosign, curl must be on PATH.
# The current kubectl context must point to the scaffolding cluster.

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# Service URLs from Knative services
FULCIO_URL=$(kubectl -n fulcio-system get ksvc fulcio -ojsonpath='{.status.url}')
REKOR_URL=$(kubectl -n rekor-system get ksvc rekor -ojsonpath='{.status.url}')
TSA_URL=$(kubectl -n tsa-system get ksvc tsa -ojsonpath='{.status.url}')
CTLOG_URL=$(kubectl -n ctlog-system get ksvc ctlog -ojsonpath='{.status.url}')

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
ISSUER_URL=$(kubectl get ksvc gettoken -ojsonpath='{.status.url}')
OIDC_TOKEN=$(curl -s "$ISSUER_URL")

# Output env vars (sourceable)
cat <<EOF
export SIGSTORE_FULCIO_URL="${FULCIO_URL}"
export SIGSTORE_REKOR_URL="${REKOR_URL}"
export SIGSTORE_TSA_URL="${TSA_URL}"
export SIGSTORE_OIDC_TOKEN="${OIDC_TOKEN}"
export SIGSTORE_TRUSTED_ROOT="${OUTDIR}/trusted_root.json"
export SIGSTORE_SIGNING_CONFIG="${OUTDIR}/signing_config.json"
export SIGSTORE_OIDC_ISSUER="https://kubernetes.default.svc.cluster.local"
export SIGSTORE_OIDC_IDENTITY="https://kubernetes.io/namespaces/default/serviceaccounts/default"
EOF
