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
[[ -n "$openscap" ]] || { echo "OPENSCAP_IMAGE not set in $repo/.env" >&2; exit 1; }
[[ -n "$base" ]] || { echo "no digest-pinned Garden Linux certs stage in $containerfile" >&2; exit 1; }

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
sha256sum "$work/ca.crt" | cut -d' ' -f1 >"$work/expected-ca.sha256"

# Root filesystem of the image under test. It is extracted inside the scanner
# as root: extracting it here as a regular user would drop setuid/setgid bits
# and ownership, which the checks inspect.
cid="$(docker create "$image" /x)"
containers+=("$cid")
docker export -o "$work/rootfs.tar" "$cid"

cp -R "$repo/.github/stig" "$work/stig"
cat >"$work/scan.sh" <<'EOF'
set -u
ds=/usr/share/xml/scap/ssg/content/ssg-chainguard-gpos-ds.xml
mkdir -p /work/out /work/rootfs
tar -C /work/rootfs -xpf /work/rootfs.tar || { echo "extract exit $?" >>/work/out/exit; exit 1; }
OSCAP_PROBE_ROOT=/work/rootfs oscap xccdf eval \
	--tailoring-file /work/stig/tailoring.xml --profile xccdf_software.ocm_profile_gpos-container \
	--results /work/out/gpos-results.xml --report /work/out/gpos-report.html "$ds" >/work/out/gpos.log 2>&1
echo "gpos exit $?" >>/work/out/exit
cd /work/stig && oscap xccdf eval --profile xccdf_software.ocm_profile_supplement \
	--results /work/out/ocm-results.xml --report /work/out/ocm-report.html ocm-supplement-xccdf.xml >/work/out/ocm.log 2>&1
echo "ocm exit $?" >>/work/out/exit
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
		echo "$failed" | sed 's/^/  FAILED: /' >&2
		status=1
	fi
done

# Per-rule output of OCM's supplementary checks.
awk '
	/<rule-result /{match($0, /idref="[^"]+"/); id = substr($0, RSTART + 7, RLENGTH - 8); sub(/^xccdf_software\.ocm_rule_/, "", id)}
	/<result>/{gsub(/.*<result>|<\/result>.*/, ""); res = $0}
	/<check-import import-name="stdout">/{inimp = 1; sub(/.*<check-import import-name="stdout">/, "")}
	inimp{line = $0; done = sub(/<\/check-import>.*/, "", line); gsub(/&lt;/, "<", line); gsub(/&gt;/, ">", line); if (line ~ /[^[:space:]]/) out = out "    " line "\n"; if (done) inimp = 0}
	/<\/rule-result>/{printf "%s: %s\n%s", id, res, out; out = ""}
' "$out/ocm-results.xml" 2>/dev/null || true
exit "$status"
