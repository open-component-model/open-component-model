#!/bin/sh
# OCM writes logs and error messages only to stdout/stderr, which the container
# runtime collects. The image must not store audit or log data on its own
# filesystem, so there is nothing for unprivileged users to read or alter.
root="${XCCDF_VALUE_ROOTFS:?}"
logs="$(find "$root" -xdev \( -path "$root/var/log" -o -name '*.log' -o -path "$root/var/log/*" \) 2>/dev/null | sed "s|^$root||")"
if [ -n "$logs" ]; then
	echo "on-disk log locations in the image:"
	echo "$logs"
	exit "$XCCDF_RESULT_FAIL"
fi
echo "no on-disk log locations; logs go to stdout/stderr"
exit "$XCCDF_RESULT_PASS"
