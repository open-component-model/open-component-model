#!/usr/bin/env bash
# Scans an OCM container image against the DISA GPOS SRG, as tailored for
# scratch images in .github/stig, and fails on any failed or errored rule.
#
#   stig-scan.sh <image> <containerfile> <output-dir>
#
# <containerfile> is the image's build file; the digest-pinned base image of its
# "certs" stage is the expected source of the CA bundle. The scan runs offline on
# the exported root filesystem: no privileged container and no docker socket.
set -euo pipefail

image="${1:?image}"
containerfile="${2:?containerfile}"
out="${3:?output directory}"
repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
openscap="$(sed -n 's/^OPENSCAP_IMAGE=//p' "$repo/.env")"
base="$(sed -nE 's/^FROM .*[[:space:]](ghcr\.io\/gardenlinux\/[^[:space:]]+@sha256:[a-f0-9]{64})[[:space:]]+AS[[:space:]]+certs$/\1/p' "$containerfile")"
entrypoint="$(docker image inspect -f '{{index .Config.Entrypoint 0}}' "$image")"
[[ -n "$openscap" ]] || { echo "OPENSCAP_IMAGE not set in $repo/.env" >&2; exit 1; }
[[ -n "$base" ]] || { echo "no digest-pinned Garden Linux certs stage in $containerfile" >&2; exit 1; }
[[ "$entrypoint" == /* ]] || { echo "image $image has no absolute entrypoint" >&2; exit 1; }

work="$(mktemp -d)"
containers=()
cleanup() {
	[[ ${#containers[@]} -gt 0 ]] && docker rm -f "${containers[@]}" >/dev/null 2>&1
	rm -rf "$work"
}
trap cleanup EXIT

# Expected CA bundle digest, taken from the pinned base image.
cid="$(docker create --platform "linux/$(docker version -f '{{.Server.Arch}}')" "$base" /bin/true)"
containers+=("$cid")
docker cp -q "$cid:/etc/ssl/certs/ca-certificates.crt" "$work/ca.crt"
ca_sha256="$(sha256sum "$work/ca.crt" | cut -d' ' -f1)"

# Root filesystem of the image under test. It is extracted inside the scanner
# as root: extracting it here as a regular user would drop setuid/setgid bits
# and ownership, which the checks inspect.
cid="$(docker create "$image" /x)"
containers+=("$cid")
docker export -o "$work/rootfs.tar" "$cid"

# Go build settings of the entrypoint, the data `go version -m` shows. OVAL
# file probes stop at the first NUL byte, so they cannot read them from the
# binary themselves.
build_setting() {
	tar -xOf "$work/rootfs.tar" "${entrypoint#/}" | LC_ALL=C grep -aoE $'^build\t'"$1"'=[^[:space:]]*' | head -1 | cut -d= -f2- || true
}
gofips140="$(build_setting GOFIPS140)"
default_godebug="$(build_setting DefaultGODEBUG)"

cp -R "$repo/.github/stig" "$work/stig"
cat >"$work/stig/values.xml" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<Tailoring xmlns="http://checklists.nist.gov/xccdf/1.2" id="xccdf_software.ocm_tailoring_supplement-values">
  <benchmark href="/work/stig/ocm-supplement-xccdf.xml"/>
  <version time="$(date -u +%Y-%m-%dT%H:%M:%S)">1</version>
  <Profile id="xccdf_software.ocm_profile_supplement-values" extends="xccdf_software.ocm_profile_supplement">
    <title>OCM GPOS SRG supplement for $image</title>
    <set-value idref="xccdf_software.ocm_value_entrypoint">$entrypoint</set-value>
    <set-value idref="xccdf_software.ocm_value_expected_ca_sha256">$ca_sha256</set-value>
    <set-value idref="xccdf_software.ocm_value_gofips140">$gofips140</set-value>
    <set-value idref="xccdf_software.ocm_value_default_godebug">$default_godebug</set-value>
  </Profile>
</Tailoring>
EOF
cat >"$work/scan.sh" <<'EOF'
set -u
ds=/usr/share/xml/scap/ssg/content/ssg-chainguard-gpos-ds.xml
mkdir -p /work/out /work/rootfs
tar -C /work/rootfs -xpf /work/rootfs.tar || { echo "extract exit $?" >>/work/out/exit; exit 1; }
# Files docker create/export add to every container; they are not image content.
rm -rf /work/rootfs/.dockerenv /work/rootfs/etc/hosts /work/rootfs/etc/hostname /work/rootfs/etc/resolv.conf \
	/work/rootfs/etc/mtab /work/rootfs/dev /work/rootfs/proc /work/rootfs/sys
cd /work/out
export OSCAP_PROBE_ROOT=/work/rootfs
oscap xccdf eval --tailoring-file /work/stig/tailoring.xml --profile xccdf_software.ocm_profile_gpos-container \
	--results gpos-results.xml --report gpos-report.html "$ds" >gpos.log 2>&1
echo "gpos exit $?" >>exit
oscap xccdf eval --tailoring-file /work/stig/values.xml --profile xccdf_software.ocm_profile_supplement-values \
	--oval-results --results ocm-results.xml --report ocm-report.html /work/stig/ocm-supplement-xccdf.xml >ocm.log 2>&1
echo "ocm exit $?" >>exit
EOF

cid="$(docker create -u 0:0 --entrypoint /bin/sh "$openscap" /work/scan.sh)"
containers+=("$cid")
docker cp -q "$work/." "$cid:/work"
docker start -a "$cid" >/dev/null
mkdir -p "$out"
docker cp -q "$cid:/work/out/." "$out"

# oscap exits 2 when a rule fails; anything else non-zero is an evaluation error.
status=0
while read -r name _ code; do
	if [[ "$code" != 0 && "$code" != 2 ]]; then
		echo "$name evaluation error (exit $code), see $out/$name.log" >&2
		status=1
	fi
done <"$out/exit"

for results in "$out/gpos-results.xml" "$out/ocm-results.xml"; do
	[[ -f "$results" ]] || { echo "missing $results" >&2; status=1; continue; }
	summary="$(grep -oE '<result>[a-z]+</result>' "$results" | sed -E 's/<\/?result>//g' | sort | uniq -c | awk '{printf "%s %s, ", $1, $2}')"
	echo "$(basename "$results" -results.xml): ${summary%, }"
	failed="$(awk -F'"' '/<rule-result /{for (i = 1; i < NF; i++) if ($i ~ /idref=$/) id = $(i + 1)} /<result>(fail|error|unknown)<\/result>/{print id}' "$results")"
	if [[ -n "$failed" ]]; then
		echo "${failed//xccdf_/  FAILED: xccdf_}" >&2
		status=1
	fi
done

# OVAL tests of OCM's supplementary checks that did not pass, by comment.
oval_results="$out/ocm-supplement-oval.xml.result.xml"
if [[ -f "$oval_results" ]]; then
	failed_tests="$(grep -oE '<test test_id="oval:software\.ocm:tst:[0-9]+"[^>]*result="(false|error|unknown)"' "$oval_results" | cut -d'"' -f2 | sort -u || true)"
	for id in $failed_tests; do
		echo "    $(grep -oE "id=\"$id\"[^>]*comment=\"[^\"]+\"" "$repo/.github/stig/ocm-supplement-oval.xml" | sed -E 's/.*comment="([^"]+)"/\1/'): failed" >&2
	done
fi
exit "$status"
