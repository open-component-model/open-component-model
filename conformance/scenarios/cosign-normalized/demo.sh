#!/usr/bin/env bash
# Host-side guided walkthrough of the normalized OCI layout (ADR 0034), the narrative
# companion to this scenario's Taskfile (which is the image-based conformance check).
# It publishes a component version that cosign can sign like an image, signs it with BOTH
# plain cosign and `ocm sign`, carries it across an airgap as a CTF (oci -> ctf -> oci),
# and verifies on the far side three ways against the same unchanged manifest digest:
# cosign, ocm verify, and cosign-only (no ocm). It doubles as the basis for the blog post.
#
# The component has two resources on purpose:
#   - notice  : a local blob (access is a content digest -> stable across transport)
#   - podinfo : an external OCI image (access is a LOCATION -> rewritten on transport)
# A localblob uploader relocates the image across the airgap, so we can watch the
# access descriptor change while D_norm (the signed digest) stays identical.
#
# Layout is selected by client config (oci.storage.config.ocm.software/v1alpha1,
# componentVersionLayout: normalized) — there is no --layout flag.
#
# Each command is printed and waits for Enter, then runs showing its real output
# (auth/plain-HTTP and tlog-skip warnings are suppressed as noise). Set PAUSE=0 to
# run straight through. Requires docker, oras, curl, jq, openssl; cosign is used
# from PATH or downloaded (and cached under $TMPDIR).
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# --- configuration (override via environment) --------------------------------
# default to the CLI built from this checkout (task bindings/go/cli:build), else `ocm` on PATH
OCM=${OCM:-"$SCRIPT_DIR/../../../bindings/go/cli/tmp/bin/ocm"}
COSIGN=${COSIGN:-}
SOURCE_PORT=${SOURCE_PORT:-15001}
TARGET_PORT=${TARGET_PORT:-15002}
ZOT_IMAGE=${ZOT_IMAGE:-ghcr.io/project-zot/zot:v2.1.22}
NAME=${NAME:-acme.org/demo}
VERSION=${VERSION:-1.0.0}
# a real upstream image, referenced by tag
PODINFO=${PODINFO:-ghcr.io/stefanprodan/podinfo:6.7.1}
PAUSE=${PAUSE:-auto}
WORK=${WORK:-$(mktemp -d "${TMPDIR:-/tmp}/ocm-airgap.XXXXXX")}
COSIGN_CACHE=${COSIGN_CACHE:-"${TMPDIR:-/tmp}/ocm-demo-cosign"}
SRC_CTR=ocm-airgap-src; DST_CTR=ocm-airgap-dst
REPO="ocm/component-descriptors/$NAME"
ACCEPT='Accept: application/vnd.oci.image.manifest.v1+json'
CFLAGS="--allow-insecure-registry --allow-http-registry"
# ocm runs at --loglevel error so auth/plain-HTTP warnings and the INFO wall are gone while the
# live progress display and real results stay; OCM= holds the binary, OCMQ adds the flag.
export COSIGN_PASSWORD=""

c() { printf '\033[%sm' "$1"; }
r() { printf '\033[0m'; }
note()  { printf '\n%s# %s%s\n' "$(c 2)" "$*" "$(r)"; }
die()   { printf '%sFAIL: %s%s\n' "$(c '1;31')" "$*" "$(r)" >&2; exit 1; }
sani()  { local s="$*"; s=${s//\"$OCM\"/ocm}; s=${s//"$OCM"/ocm}
          s=${s//\"$COSIGN\"/cosign}; s=${s//"$COSIGN"/cosign}; printf '%s' "$s"; }
_wait() { case "$PAUSE" in 0|no|false) printf '\n'; return;; esac
          if [ -t 0 ]; then read -r _ || true; else printf '\n'; fi; }
# print the command, wait for Enter, run it — stderr left on the TTY so ocm's live progress shows
run()   { printf '%s$ %s%s' "$(c '1;36')" "$(sani "$*")" "$(r)"; _wait; eval "$*"; }
# cosign has no progress display, so filter its one noisy line (the offline tlog-skip notice)
runc()  { printf '%s$ %s%s' "$(c '1;36')" "$(sani "$*")" "$(r)"; _wait
          eval "$*" 2> >(grep -vE 'Skipping tlog verification' >&2 || true); }

cleanup() { docker rm -f "$SRC_CTR" "$DST_CTR" >/dev/null 2>&1 || true; }
trap cleanup EXIT

D() { curl -sSfI -H "$ACCEPT" "http://127.0.0.1:$1/v2/$REPO/manifests/$VERSION" \
        | awk 'tolower($1)=="docker-content-digest:"{print $2}' | tr -d '\r'; }

# --- preflight ---------------------------------------------------------------
for t in docker oras curl jq openssl; do command -v "$t" >/dev/null || die "$t is required"; done
[ -x "$OCM" ] || command -v "$OCM" >/dev/null || { command -v ocm >/dev/null && OCM=ocm || die "ocm not found: build it with 'task bindings/go/cli:build' or set OCM=<path>"; }
if [ -n "$COSIGN" ] && ! { [ -x "$COSIGN" ] || command -v "$COSIGN" >/dev/null; }; then
  note "ignoring COSIGN='$COSIGN' (not executable); using PATH or a cached download"; COSIGN=""
fi
if [ -z "$COSIGN" ]; then
  if command -v cosign >/dev/null; then COSIGN=cosign
  elif [ -x "$COSIGN_CACHE" ]; then COSIGN="$COSIGN_CACHE"
  else
    os=$(uname -s|tr '[:upper:]' '[:lower:]'); arch=$(uname -m)
    case "$arch" in arm64|aarch64) arch=arm64;; x86_64|amd64) arch=amd64;; esac
    note "downloading cosign ($os/$arch) to $COSIGN_CACHE ..."
    curl -fsSL --retry 3 --connect-timeout 10 -o "$COSIGN_CACHE" \
      "https://github.com/sigstore/cosign/releases/latest/download/cosign-$os-$arch"
    chmod +x "$COSIGN_CACHE"; COSIGN="$COSIGN_CACHE"
  fi
fi

SRC="oci::127.0.0.1:$SOURCE_PORT/ocm"; DST="oci::127.0.0.1:$TARGET_PORT/ocm"
CV_SRC="$SRC//$NAME:$VERSION"; CV_DST="$DST//$NAME:$VERSION"
ORAS_SRC="--plain-http 127.0.0.1:$SOURCE_PORT/$REPO"
ORAS_DST="--plain-http 127.0.0.1:$TARGET_PORT/$REPO"

note "versions"
run "\"$OCM\" --version"
runc "\"$COSIGN\" version"

# --- registries + sources + config -------------------------------------------
note "starting two zot registries (connected source :$SOURCE_PORT, airgapped target :$TARGET_PORT)"
cleanup
cat > "$WORK/zot.json" <<'EOF'
{"storage":{"rootDirectory":"/var/lib/registry"},"http":{"address":"0.0.0.0","port":"5000"},"log":{"level":"warn"}}
EOF
docker run -d --name "$SRC_CTR" -p "127.0.0.1:$SOURCE_PORT:5000" -v "$WORK/zot.json":/etc/zot/config.json:ro "$ZOT_IMAGE" >/dev/null
docker run -d --name "$DST_CTR" -p "127.0.0.1:$TARGET_PORT:5000" -v "$WORK/zot.json":/etc/zot/config.json:ro "$ZOT_IMAGE" >/dev/null
for p in "$SOURCE_PORT" "$TARGET_PORT"; do ready=""
  for _ in $(seq 1 60); do curl -sf "http://127.0.0.1:$p/v2/" >/dev/null && { ready=1; break; }; sleep 0.5; done
  [ -n "$ready" ] || die "registry on 127.0.0.1:$p did not become ready"; done
cd "$WORK"
echo "signed with cosign, carried by ocm transfer" > notice.txt
cat > component-constructor.yaml <<EOF
components:
  - name: $NAME
    version: $VERSION
    provider: { name: acme.org }
    resources:
      # a local blob: access is a content digest, so it is stable across transport
      - name: notice
        type: plainText
        relation: local
        input: { type: File/v1, path: ./notice.txt, mediaType: text/plain }
      # an external upstream image: access is a LOCATION, rewritten when relocated on transport
      - name: podinfo
        type: ociImage
        version: $VERSION
        access: { type: OCIImage/v1, imageReference: $PODINFO }
EOF
openssl genpkey -algorithm RSA -out rsa-private.pem -pkeyopt rsa_keygen_bits:3072 2>/dev/null
openssl rsa -pubout -in rsa-private.pem -out rsa-public.pem 2>/dev/null
cat > scenario.ocmconfig <<'EOF'
type: generic.config.ocm.software/v1
configurations:
  - type: oci.storage.config.ocm.software/v1alpha1
    componentVersionLayout: normalized
  - type: http.config.ocm.software/v1alpha1
    plainHTTP: true
  # relocate non-local resources (the image) into the transport as local blobs,
  # so they cross the airgap and their access is rewritten for the target
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
EOF
printf 'type: generic.config.ocm.software/v1\nconfigurations:\n  - type: credentials.config.ocm.software\n    consumers:\n      - identity: {type: RSA/v1alpha1, algorithm: RSASSA-PSS, signature: default}\n        credentials: [{type: Credentials/v1, properties: {private_key_pem_file: %s/rsa-private.pem}}]\n' "$WORK" > private.ocmconfig
printf 'type: generic.config.ocm.software/v1\nconfigurations:\n  - type: credentials.config.ocm.software\n    consumers:\n      - identity: {type: RSA/v1alpha1, algorithm: RSASSA-PSS, signature: default}\n        credentials: [{type: Credentials/v1, properties: {public_key_pem_file: %s/rsa-public.pem}}]\n' "$WORK" > public.ocmconfig
note "the normalized layout is selected by config (no --layout flag):"; sed 's/^/    /' scenario.ocmconfig

# --- publish -----------------------------------------------------------------
note "publish the component version in the normalized layout (connected source)"
run "\"$OCM\" add componentversion --loglevel error --config scenario.ocmconfig --repository $SRC --constructor component-constructor.yaml"

note "the tag resolves to the normalized manifest; its digest D_norm is what cosign signs"
run "curl -sSfI -H '$ACCEPT' http://127.0.0.1:$SOURCE_PORT/v2/$REPO/manifests/$VERSION"
D_NORM=$(D "$SOURCE_PORT"); [ -n "$D_NORM" ] || die "no D_norm"

note "the single signed layer N — access-free: resource DIGESTS but no access, for BOTH resources"
N=$(oras manifest fetch "127.0.0.1:$SOURCE_PORT/$REPO:$VERSION" --plain-http | jq -r .layers[0].digest)
run "oras blob fetch --output - $ORAS_SRC@$N | jq '.component.resources'"

note "the access-bearing descriptor OCM resolves on the SOURCE (resolves the access referrer + bind check):"
run "\"$OCM\" get componentversion --loglevel error --config scenario.ocmconfig $CV_SRC -o json | jq '.[].component.resources[] | {name, access}'"
note "notice = LocalBlob (a content digest); podinfo = OCIImage pointing at an upstream registry"

# --- sign (connected): cosign then ocm ---------------------------------------
SRC_REF="127.0.0.1:$SOURCE_PORT/$REPO@$D_NORM"
note "cosign v3: an offline signing config (no transparency-log services) avoids the deprecated flags"
runc "\"$COSIGN\" signing-config create --out signing-config.json"
runc "\"$COSIGN\" generate-key-pair"
runc "\"$COSIGN\" sign --key cosign.key --signing-config signing-config.json $CFLAGS --yes $SRC_REF"
runc "\"$COSIGN\" verify --key cosign.pub --insecure-ignore-tlog=true $CFLAGS $SRC_REF"
note "the cosign signature is now a referrer of D_norm (sigstore bundle), beside the access descriptor:"
run "oras discover $ORAS_SRC@$D_NORM --format tree"

note "add an OCM signature too — this must NOT move the tag off D_norm"
run "\"$OCM\" sign componentversion --loglevel error --config private.ocmconfig --config scenario.ocmconfig $CV_SRC"
after=$(D "$SOURCE_PORT"); [ "$after" = "$D_NORM" ] || die "ocm sign moved the tag to $after"
note "tag still resolves to D_norm = $D_NORM"
runc "\"$COSIGN\" verify --key cosign.pub --insecure-ignore-tlog=true $CFLAGS $SRC_REF"
run "\"$OCM\" verify componentversion --loglevel error --config public.ocmconfig --config scenario.ocmconfig $CV_SRC && echo '✓ OCM signature verification successful'"

# --- airgap: oci -> ctf -> oci -----------------------------------------------
note "export across the airgap: source registry -> CTF (the image is relocated into the archive)"
rm -rf transport.ctf
run "\"$OCM\" transfer componentversion --loglevel error --config scenario.ocmconfig $CV_SRC ctf::./transport.ctf"
note "import on the far side: CTF -> airgapped target registry"
run "\"$OCM\" transfer componentversion --loglevel error --config scenario.ocmconfig ctf::./transport.ctf//$NAME:$VERSION $DST"
tgt=$(D "$TARGET_PORT"); [ "$tgt" = "$D_NORM" ] || die "target D_norm $tgt != $D_NORM"
note "D_norm preserved across OCI -> CTF -> OCI: $tgt"
run "oras discover $ORAS_DST@$D_NORM --format tree"
note "the access-bearing descriptor on the TARGET:"
run "\"$OCM\" get componentversion --loglevel error --config scenario.ocmconfig $CV_DST -o json | jq '.[].component.resources[] | {name, access}'"
note "podinfo is now a LocalBlob in the target (access REWRITTEN); notice unchanged; yet D_norm is identical"

# --- verify on the airgapped target, three ways ------------------------------
note "1/3  cosign — standard sigstore tooling, no re-signing"
runc "\"$COSIGN\" verify --key cosign.pub --insecure-ignore-tlog=true $CFLAGS 127.0.0.1:$TARGET_PORT/$REPO@$D_NORM"
note "2/3  ocm verify — the OCM in-descriptor signature"
run "\"$OCM\" verify componentversion --loglevel error --config public.ocmconfig --config scenario.ocmconfig $CV_DST && echo '✓ OCM signature verification successful'"

note "3/3  cosign-only, NO ocm — authenticate, then read the signed descriptor and bind content"
runc "\"$COSIGN\" verify --key cosign.pub --insecure-ignore-tlog=true $CFLAGS 127.0.0.1:$TARGET_PORT/$REPO@$D_NORM"
layer=$(curl -sSf -H "$ACCEPT" "http://127.0.0.1:$TARGET_PORT/v2/$REPO/manifests/$D_NORM" | jq -r '.layers[0].digest')
note "the signed descriptor, read straight from the verified manifest (access-free):"
run "curl -sSf http://127.0.0.1:$TARGET_PORT/v2/$REPO/blobs/$layer | jq ."
descriptor=$(curl -sSf "http://127.0.0.1:$TARGET_PORT/v2/$REPO/blobs/$layer")
[ "$(echo "$descriptor" | jq -r '.component.name')" = "$NAME" ] || die "not $NAME"
[ "$(echo "$descriptor" | jq '[.component.resources[]|select(.access!=null)]|length')" = "0" ] || die "not access-free"
note "every resource digest in that descriptor is covered by the cosign signature"
for e in $(echo "$descriptor" | jq -r '.component.resources[]|select(.digest.normalisationAlgorithm=="genericBlobDigest/v1")|.name+"="+.digest.value'); do
  n=${e%%=*}; d=${e##*=}
  note "local blob resource '$n': fetch the blob by its signed digest and re-hash it"
  run "curl -sSf http://127.0.0.1:$TARGET_PORT/v2/$REPO/blobs/sha256:$d | openssl dgst -sha256"
  note "expected (from the signed descriptor): $d"
  got=$(curl -sSf "http://127.0.0.1:$TARGET_PORT/v2/$REPO/blobs/sha256:$d" | openssl dgst -sha256 | awk '{print $NF}')
  [ "$got" = "$d" ] || die "resource $n content $got != signed digest $d"
done
hd=$(echo "$descriptor" | jq -r '.component.resources[]|select(.digest.normalisationAlgorithm=="ociArtifactDigest/v1")|.digest.value' | head -1)
if [ -n "$hd" ]; then
  note "the image resource is bound by its manifest digest (ociArtifactDigest); the registry serves it content-addressed:"
  run "curl -sSfI http://127.0.0.1:$TARGET_PORT/v2/$REPO/manifests/sha256:$hd"
  got=$(curl -sSfI -H "$ACCEPT" "http://127.0.0.1:$TARGET_PORT/v2/$REPO/manifests/sha256:$hd" | awk 'tolower($1)=="docker-content-digest:"{print $2}' | tr -d '\r')
  [ "$got" = "sha256:$hd" ] || die "image manifest digest $got != signed digest sha256:$hd"
fi

note "and OCM can still download a resource normally:"
run "\"$OCM\" download resource --loglevel error --config public.ocmconfig --config scenario.ocmconfig $CV_DST --identity name=notice --output got.txt; cat got.txt"

printf '\n%sAll checks passed.%s D_norm %s stayed stable through OCI -> CTF -> OCI;\n' "$(c '1;32')" "$(r)" "$D_NORM"
echo "the image's access was rewritten on transport, but its content digest (in the signed descriptor) did not change,"
echo "so cosign, ocm verify, and cosign-only verification all succeed on the airgapped target."
echo "Work dir (keys, CTF, configs): $WORK   (registries removed on exit)"
