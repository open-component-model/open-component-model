#!/bin/sh
# The image must contain no shared libraries, no package manager, no
# setuid/setgid files, and no world-writable location without the sticky bit.
# With a static binary and no package manager, library permissions cannot be
# changed and users cannot install software.
root="${XCCDF_VALUE_ROOTFS:?}"
rc=$XCCDF_RESULT_PASS
report() {
	[ -z "$2" ] && return
	echo "$1:"
	echo "$2" | sed "s|^$root|  |"
	rc=$XCCDF_RESULT_FAIL
}
report "shared libraries" "$(find "$root" -xdev -type f \( -name '*.so' -o -name '*.so.*' \) 2>/dev/null)"
report "dynamic loaders" "$(find "$root" -xdev -name 'ld-linux*' -o -xdev -name 'ld-musl*' 2>/dev/null)"
report "package managers or package databases" "$(find "$root" -xdev \( -path "$root/var/lib/dpkg" -o -path "$root/var/lib/rpm" -o -path "$root/lib/apk" -o -name apt -o -name apt-get -o -name dpkg -o -name rpm -o -name dnf -o -name yum -o -name apk -o -name zypper \) 2>/dev/null)"
report "setuid/setgid files" "$(find "$root" -xdev -type f \( -perm -4000 -o -perm -2000 \) 2>/dev/null)"
report "world-writable files" "$(find "$root" -xdev -type f -perm -0002 2>/dev/null)"
report "world-writable directories without the sticky bit" "$(find "$root" -xdev -type d -perm -0002 ! -perm -1000 2>/dev/null)"
[ "$rc" = "$XCCDF_RESULT_PASS" ] && echo "no shared libraries, package manager, setuid/setgid or unprotected world-writable paths"
exit "$rc"
