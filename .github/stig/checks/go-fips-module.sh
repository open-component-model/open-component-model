#!/bin/sh
# Every Go executable in the image must be built against a frozen Go
# Cryptographic Module (GOFIPS140=v<version>) and default to fips140=on. This is
# the build-information check the module's Security Policy (section 11.1) names
# for a correctly configured binary. OCM's cryptography runs only in that module,
# so it replaces the GPOS SRG checks for an OpenSSL FIPS provider.
root="${XCCDF_VALUE_ROOTFS:?}"
found=0
rc=$XCCDF_RESULT_PASS
executables="$(find "$root" -xdev -type f -perm -u+x 2>/dev/null)"
while IFS= read -r f; do
	[ -n "$f" ] || continue
	grep -aq 'Go buildinf:' "$f" || continue
	found=1
	name="${f#"$root"}"
	if ! grep -aq "$(printf 'build\tGOFIPS140=v')" "$f"; then
		echo "$name: not built with GOFIPS140=v<version>"
		rc=$XCCDF_RESULT_FAIL
	fi
	if ! grep -aqE "$(printf 'build\tDefaultGODEBUG=')(.*,)?fips140=(on|only)(,|$)" "$f"; then
		echo "$name: DefaultGODEBUG does not enable fips140"
		rc=$XCCDF_RESULT_FAIL
	fi
	[ "$rc" = "$XCCDF_RESULT_PASS" ] && echo "$name: $(grep -aoE "$(printf 'build\tGOFIPS140=')v[^[:space:]]+" "$f" | head -1 | cut -f2)"
done <<EOF
$executables
EOF
if [ "$found" = 0 ]; then
	echo "no Go executable found in the image"
	exit "$XCCDF_RESULT_FAIL"
fi
exit "$rc"
