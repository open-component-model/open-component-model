#!/bin/sh
# The image's trust anchors must be exactly the CA bundle of the digest-pinned
# Garden Linux FIPS base image. The scan script extracts the expected digest
# from that image; the path comes from the XCCDF value EXPECTED_CA_SHA256.
root="${XCCDF_VALUE_ROOTFS:?}"
bundle="$root/etc/ssl/certs/ca-certificates.crt"
expected="$(cat "${XCCDF_VALUE_EXPECTED_CA_SHA256:?}")"
if [ ! -f "$bundle" ]; then
	echo "/etc/ssl/certs/ca-certificates.crt is missing"
	exit "$XCCDF_RESULT_FAIL"
fi
actual="$(sha256sum "$bundle" | cut -d' ' -f1)"
if [ "$actual" != "$expected" ]; then
	echo "CA bundle sha256 $actual does not match the pinned base image ($expected)"
	exit "$XCCDF_RESULT_FAIL"
fi
other="$(find "$root" -xdev -type f \( -name '*.pem' -o -name '*.crt' -o -name '*.cer' \) ! -path "$bundle" 2>/dev/null | sed "s|^$root||")"
if [ -n "$other" ]; then
	echo "additional certificate files outside the pinned bundle: $other"
	exit "$XCCDF_RESULT_FAIL"
fi
echo "CA bundle matches the pinned base image ($actual)"
exit "$XCCDF_RESULT_PASS"
