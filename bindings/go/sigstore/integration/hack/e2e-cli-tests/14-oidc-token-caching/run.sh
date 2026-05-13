#!/usr/bin/env bash

# Test 14: OIDC Token Caching
# Signs a component 3 times in a row. The browser should only open ONCE —
# the second and third signs should use the cached refresh token silently.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../_lib/helpers.sh"

require_command cosign

cd "$SCRIPT_DIR"

# Clear any existing OIDC token cache so we start fresh
log_step "Clearing OIDC token cache"
rm -rf "${HOME}/.ocm/cache/oidc" 2>/dev/null || true

# Reset TUF cache to public Sigstore
log_step "Resetting cosign TUF cache to public Sigstore"
cosign initialize 2>/dev/null || true

# Create 3 separate CTFs (each needs its own unsigned component)
for i in 1 2 3; do
  rm -rf "./ctf-${i}"
  "$OCM_BIN" add component-version \
    --repository "ctf::./ctf-${i}" \
    --constructor ./component-constructor.yaml
done

echo ""
echo "================================================================"
echo " OIDC Token Caching Test"
echo "================================================================"
echo ""
echo " This test signs 3 component versions in sequence."
echo " The browser should open ONLY for the FIRST sign operation."
echo " Signs 2 and 3 should complete silently (cached refresh token)."
echo ""
echo "================================================================"
echo ""

# Sign #1 — browser will open
log_step "[Sign 1/3] Signing (browser WILL open for authentication)..."
time "$OCM_BIN" sign cv "ctf::./ctf-1//ocm.software/sigstore-e2e-test:v1.0.0" \
  --config ./ocmconfig.yaml \
  --signer-spec ./signer-spec.yaml \
  --force
echo ""

# Check cache was created
CACHE_DIR="${HOME}/.ocm/cache/oidc"

if [[ -d "$CACHE_DIR" ]] && ls "$CACHE_DIR"/*.json &>/dev/null; then
  log_pass "Token cache file created at $CACHE_DIR"
else
  log_fail "No token cache file found at $CACHE_DIR"
  exit 1
fi

echo ""

# Sign #2 — should use cached token (NO browser)
log_step "[Sign 2/3] Signing (should be SILENT — no browser)..."
time "$OCM_BIN" sign cv "ctf::./ctf-2//ocm.software/sigstore-e2e-test:v1.0.0" \
  --config ./ocmconfig.yaml \
  --signer-spec ./signer-spec.yaml \
  --force
log_pass "Sign 2 completed"
echo ""

# Sign #3 — should use cached token (NO browser)
log_step "[Sign 3/3] Signing (should be SILENT — no browser)..."
time "$OCM_BIN" sign cv "ctf::./ctf-3//ocm.software/sigstore-e2e-test:v1.0.0" \
  --config ./ocmconfig.yaml \
  --signer-spec ./signer-spec.yaml \
  --force
log_pass "Sign 3 completed"

echo ""
echo "================================================================"
echo ""
log_pass "All 3 signs completed. If the browser only opened once, caching works!"
echo ""
echo "================================================================"

# Cleanup
rm -rf ./ctf-1 ./ctf-2 ./ctf-3
