#!/usr/bin/env bash
# Three-minute live demo of the "Sign, Transfer, Verify" section of the blog post
# website/content/blog/2026-11-26-cosign-signable-components.md: publish a component version
# with an image reference in the normalized layout, sign it with plain cosign, transfer it
# (image included) to a second registry, and verify it there with plain cosign.
# demo.sh is the long walkthrough (CTF airgap, ocm sign, cosign-only verification).
#
# The commands on screen are the ones of the post. Two things are local to this demo and kept
# out of sight by the ocm/cosign/oras wrappers below:
#   - the registries are plain-HTTP zot containers on source.localhost / target.localhost;
#   - cosign signs and verifies offline, without a transparency log (and its warning is hidden).
#
# Each command is printed and waits for Enter; PAUSE=0 runs straight through.
# Requires docker, oras, cosign, yq. The podinfo image is cached under $CACHE after the first
# run, so a later run needs no internet (the zot image must be pulled already).
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# absolute paths: the wrapper functions below shadow the bare names
OCM=$(command -v "${OCM:-$SCRIPT_DIR/../../../bindings/go/cli/tmp/bin/ocm}" || true)
COSIGN=$(command -v "${COSIGN:-cosign}" || true)
SOURCE=source.localhost:${SOURCE_PORT:-5001}
TARGET=target.localhost:${TARGET_PORT:-5002}
ZOT_IMAGE=${ZOT_IMAGE:-ghcr.io/project-zot/zot:v2.1.22}
PODINFO=${PODINFO:-ghcr.io/stefanprodan/podinfo:6.7.1}
PAUSE=${PAUSE:-1}
CACHE=${CACHE:-"${TMPDIR:-/tmp}/ocm-demo-short-cache"}
WORK=$(mktemp -d "${TMPDIR:-/tmp}/ocm-demo-short.XXXXXX")
SRC_CTR=ocm-demo-short-src; DST_CTR=ocm-demo-short-dst
CV=component-descriptors/acme.org/demo
export COSIGN_PASSWORD=""

die()  { printf '\033[1;31mFAIL: %s\033[0m\n' "$*" >&2; exit 1; }
note() { printf '\n\033[2m# %s\033[0m\n' "$*"; }
# print the command, wait for Enter, run it
run()  { printf '\033[1;36m$ %s\033[0m' "$*"; if [ "$PAUSE" != 0 ] && [ -t 0 ]; then read -r _; else printf '\n'; fi; eval "$*"; }

# wrappers: add what only this local setup needs, so the commands on screen stay those of the post
ocm()    { "$OCM" --loglevel error --config "$WORK/local.ocmconfig" "$@"; }
oras()   { command oras "$@" --plain-http; }
cosign() { local sub=$1; shift
  case "$sub" in
    sign)   set -- sign --yes --signing-config "$WORK/signing-config.json" --allow-http-registry "$@" ;;
    verify) set -- verify --insecure-ignore-tlog=true --allow-http-registry "$@" ;;
    *)      set -- "$sub" "$@" ;;
  esac
  "$COSIGN" "$@" 2>&1 | grep -v 'Skipping tlog verification'; }

cleanup() { docker rm -f "$SRC_CTR" "$DST_CTR" >/dev/null 2>&1 || true; }
trap cleanup EXIT

# --- setup (not part of the story) --------------------------------------------
for t in docker yq oras; do command -v "$t" >/dev/null || die "$t is required"; done
[ -x "$COSIGN" ] || die "cosign is required (or set COSIGN=<path>)"
[ -x "$OCM" ] || die "ocm not found: run 'task bindings/go/cli:build' or set OCM=<path>"
cleanup; cd "$WORK"
echo '{"storage":{"rootDirectory":"/var/lib/registry"},"http":{"address":"0.0.0.0","port":"5000"},"log":{"level":"warn"}}' > zot.json
docker run -d --name "$SRC_CTR" -p "127.0.0.1:${SOURCE#*:}:5000" -v "$WORK/zot.json":/etc/zot/config.json:ro "$ZOT_IMAGE" >/dev/null
docker run -d --name "$DST_CTR" -p "127.0.0.1:${TARGET#*:}:5000" -v "$WORK/zot.json":/etc/zot/config.json:ro "$ZOT_IMAGE" >/dev/null
for r in "$SOURCE" "$TARGET"; do ready=""
  for _ in $(seq 1 60); do curl -sf "http://$r/v2/" >/dev/null && { ready=1; break; }; sleep 0.5; done
  [ -n "$ready" ] || die "registry $r did not become ready"; done
[ -f "$CACHE/index.json" ] || { mkdir -p "$CACHE"; command oras cp "$PODINFO" --to-oci-layout "$CACHE:podinfo" >/dev/null; }
command oras cp --from-oci-layout "$CACHE:podinfo" --to-plain-http "$SOURCE/stefanprodan/podinfo:6.7.1" >/dev/null
"$COSIGN" signing-config create --out signing-config.json >/dev/null 2>&1
"$COSIGN" generate-key-pair >/dev/null 2>&1
printf 'type: generic.config.ocm.software/v1\nconfigurations:\n  - type: http.config.ocm.software/v1alpha1\n    plainHTTP: true\n' > local.ocmconfig
cp "$SCRIPT_DIR/notice.txt" notice.txt
cat > component-constructor.yaml <<EOF
components:
  - name: acme.org/demo
    version: 1.0.0
    provider: { name: acme.org }
    resources:
      - name: notice
        type: plainText
        relation: local
        input: { type: File/v1, path: ./notice.txt, mediaType: text/plain }
      - name: podinfo
        type: ociImage
        version: 6.7.1
        access: { type: OCIImage/v1, imageReference: $SOURCE/stefanprodan/podinfo:6.7.1 }
EOF
cat > ocmconfig.yaml <<'EOF'
type: generic.config.ocm.software/v1
configurations:
  # store component versions as normalized descriptor + access descriptor
  - type: oci.storage.config.ocm.software/v1alpha1
    componentVersionLayout: normalized
  # take referenced images along on transfer
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
EOF
[ -t 1 ] && clear

# --- the story -----------------------------------------------------------------
note "opt in to the normalized layout"
run "cat ocmconfig.yaml"

note "publish the component: a local blob and an image in the source registry"
run "ocm add componentversion --config ocmconfig.yaml --repository oci::$SOURCE --constructor component-constructor.yaml -o yaml"

note "the manifest digest of the component version in the source registry"
run "oras resolve $SOURCE/$CV:1.0.0"
DIGEST=$(oras resolve "$SOURCE/$CV:1.0.0")

note "sign that digest with plain cosign"
run "cosign sign --key cosign.key $SOURCE/$CV@$DIGEST"

note "transfer the component, and the image with it"
run "ocm transfer componentversion --config ocmconfig.yaml oci::$SOURCE//acme.org/demo:1.0.0 oci::$TARGET"

note "the access of the image was rewritten: source, then target"
run "ocm get componentversion oci::$SOURCE//acme.org/demo:1.0.0 -o yaml | yq '.[].component.resources[1].access'"
run "ocm get componentversion oci::$TARGET//acme.org/demo:1.0.0 -o yaml | yq '.[].component.resources[1].access'"

note "the manifest digest in the target registry is still the same"
run "oras resolve $TARGET/$CV:1.0.0"
[ "$(oras resolve "$TARGET/$CV:1.0.0")" = "$DIGEST" ] || die "digest changed on transfer"

note "the signature travelled along: verify in the target registry with plain cosign"
run "cosign verify --key cosign.pub $TARGET/$CV@$DIGEST"
