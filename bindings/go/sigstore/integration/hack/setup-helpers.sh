# shellcheck shell=bash
# Sourced helper library for sigstore scaffolding scripts.
# No top-level set directives: callers already run under set -euo pipefail
# and this file must not alter those options when sourced.

# download_file URL DEST
#
# Downloads URL to DEST atomically.  A temp file in the same directory as
# DEST is created first so the final mv is atomic (same filesystem, single
# rename syscall).  Every command is checked explicitly because callers often
# invoke this function in if/|| context where errexit is suppressed.
#
# Return codes: 0 = ok, 4 = HTTP 404, 1 = any other failure or empty body.
download_file() {
  local url="$1"
  local dest="$2"
  local dir
  dir="$(dirname "$dest")"
  if ! mkdir -p "$dir"; then
    echo "sigstore setup: download failed: $url" >&2
    return 1
  fi
  local tmp
  if ! tmp="$(mktemp "$dir/.$(basename "$dest").XXXXXX")"; then
    echo "sigstore setup: download failed: $url" >&2
    return 1
  fi
  local http_code
  http_code=""
  # --retry-all-errors is intentionally absent: 404 and TLS errors must
  # surface immediately rather than consuming the full retry budget.
  # HTTP 503 is still retried because curl treats Service Unavailable as
  # transient by default.
  if ! http_code=$(curl --fail --silent --show-error --location \
      --connect-timeout 10 --max-time 60 \
      --retry 5 --retry-connrefused --retry-max-time 180 \
      --write-out '%{http_code}' --output "$tmp" "$url"); then
    rm -f "$tmp"
    if [ "$http_code" = "404" ]; then
      echo "sigstore setup: not found: $url" >&2
      return 4
    elif [ -n "$http_code" ] && [ "$http_code" != "000" ]; then
      echo "sigstore setup: download failed: $url (HTTP $http_code)" >&2
    else
      echo "sigstore setup: download failed: $url" >&2
    fi
    return 1
  fi
  if [ ! -s "$tmp" ]; then
    rm -f "$tmp"
    echo "sigstore setup: empty download: $url" >&2
    return 1
  fi
  if ! mv -f "$tmp" "$dest"; then
    rm -f "$tmp"
    echo "sigstore setup: download failed: $url" >&2
    return 1
  fi
}

# write_scaffolding_hosts HOSTS_FILE IP HOSTNAME...
#
# Writes or replaces the managed OCM sigstore scaffolding block in HOSTS_FILE.
# All lines outside the block are preserved in their original order.
# An unmanaged line that binds a requested hostname to a different IP is
# rejected as a conflict; the same IP in an unmanaged line is allowed.
#
# The write is done via cat "$tmp" > HOSTS_FILE so the file's inode, owner
# and mode are preserved, which matters when writing /etc/hosts as root.
write_scaffolding_hosts() {
  local hosts_file="$1"
  local ip="$2"
  shift 2

  if [ ! -f "$hosts_file" ] || [ ! -r "$hosts_file" ]; then
    echo "sigstore setup: hosts file missing or unreadable: $hosts_file" >&2
    return 1
  fi

  if ! printf '%s\n' "$ip" | grep -qE '^[0-9]{1,3}(\.[0-9]{1,3}){3}$'; then
    echo "sigstore setup: invalid IP: $ip" >&2
    return 1
  fi

  if [ "$#" -eq 0 ]; then
    echo "sigstore setup: no hostnames provided" >&2
    return 1
  fi

  local h
  for h in "$@"; do
    if ! printf '%s\n' "$h" | grep -qE '^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$'; then
      echo "sigstore setup: invalid hostname: $h" >&2
      return 1
    fi
  done

  # Verify managed block structure before any mutation attempt.
  local b_count=0
  local e_count=0
  if ! b_count=$(grep -c '^# BEGIN OCM SIGSTORE SCAFFOLDING$' "$hosts_file"); then
    b_count=0
  fi
  if ! e_count=$(grep -c '^# END OCM SIGSTORE SCAFFOLDING$' "$hosts_file"); then
    e_count=0
  fi

  if [ "$b_count" -ne "$e_count" ] || [ "$b_count" -gt 1 ]; then
    echo "sigstore setup: malformed managed hosts block" >&2
    return 1
  fi

  if [ "$b_count" -eq 1 ]; then
    local b_line e_line
    b_line=$(grep -n '^# BEGIN OCM SIGSTORE SCAFFOLDING$' "$hosts_file" | head -1 | cut -d: -f1)
    e_line=$(grep -n '^# END OCM SIGSTORE SCAFFOLDING$' "$hosts_file" | head -1 | cut -d: -f1)
    if [ "$e_line" -le "$b_line" ]; then
      echo "sigstore setup: malformed managed hosts block" >&2
      return 1
    fi
  fi

  # Build a pipe-separated hostname list for awk; validation above ensures
  # no hostname contains a pipe character.
  local hosts_joined=""
  for h in "$@"; do
    if [ -z "$hosts_joined" ]; then
      hosts_joined="$h"
    else
      hosts_joined="${hosts_joined}|${h}"
    fi
  done

  # Reject unmanaged lines that bind a requested hostname to a different IP.
  # The same IP in an unmanaged line is not a conflict.
  local conflict_host=""
  if ! conflict_host=$(awk \
      -v "b_mark=# BEGIN OCM SIGSTORE SCAFFOLDING" \
      -v "e_mark=# END OCM SIGSTORE SCAFFOLDING" \
      -v "managed_ip=$ip" \
      -v "hlist=$hosts_joined" \
      '
      BEGIN {
        in_block = 0
        n = split(hlist, ha, "|")
        for (i = 1; i <= n; i++) want[ha[i]] = 1
      }
      $0 == b_mark { in_block = 1; next }
      $0 == e_mark { in_block = 0; next }
      !in_block && !/^[[:space:]]*#/ && NF >= 2 && $1 != managed_ip {
        for (i = 2; i <= NF; i++) {
          if ($i in want) {
            print $i
            exit
          }
        }
      }
      ' "$hosts_file"); then
    return 1
  fi

  if [ -n "$conflict_host" ]; then
    echo "sigstore setup: conflicting hosts entry for $conflict_host" >&2
    return 1
  fi

  local tmp
  if ! tmp="$(mktemp "${TMPDIR:-/tmp}/ocm-hosts.XXXXXX")"; then
    return 1
  fi

  # Emit non-block lines in original order, then the new block.
  if ! awk \
      -v "b_mark=# BEGIN OCM SIGSTORE SCAFFOLDING" \
      -v "e_mark=# END OCM SIGSTORE SCAFFOLDING" \
      -v "managed_ip=$ip" \
      -v "hlist=$hosts_joined" \
      '
      BEGIN { in_block = 0 }
      $0 == b_mark { in_block = 1; next }
      $0 == e_mark { in_block = 0; next }
      !in_block { print }
      END {
        print "# BEGIN OCM SIGSTORE SCAFFOLDING"
        n = split(hlist, ha, "|")
        for (i = 1; i <= n; i++) {
          print managed_ip " " ha[i]
        }
        print "# END OCM SIGSTORE SCAFFOLDING"
      }
      ' "$hosts_file" > "$tmp"; then
    rm -f "$tmp"
    return 1
  fi

  if ! cat "$tmp" > "$hosts_file"; then
    rm -f "$tmp"
    return 1
  fi
  rm -f "$tmp"
}

# remove_scaffolding_hosts HOSTS_FILE
#
# Removes the managed OCM sigstore scaffolding block from HOSTS_FILE.
# If no block is present the file is left untouched (no-op, return 0).
# Uses the same write method as write_scaffolding_hosts to preserve inode.
remove_scaffolding_hosts() {
  local hosts_file="$1"

  if [ ! -f "$hosts_file" ] || [ ! -r "$hosts_file" ]; then
    echo "sigstore setup: hosts file missing or unreadable: $hosts_file" >&2
    return 1
  fi

  local b_count=0
  local e_count=0
  if ! b_count=$(grep -c '^# BEGIN OCM SIGSTORE SCAFFOLDING$' "$hosts_file"); then
    b_count=0
  fi
  if ! e_count=$(grep -c '^# END OCM SIGSTORE SCAFFOLDING$' "$hosts_file"); then
    e_count=0
  fi

  if [ "$b_count" -ne "$e_count" ] || [ "$b_count" -gt 1 ]; then
    echo "sigstore setup: malformed managed hosts block" >&2
    return 1
  fi

  if [ "$b_count" -eq 1 ]; then
    local b_line e_line
    b_line=$(grep -n '^# BEGIN OCM SIGSTORE SCAFFOLDING$' "$hosts_file" | head -1 | cut -d: -f1)
    e_line=$(grep -n '^# END OCM SIGSTORE SCAFFOLDING$' "$hosts_file" | head -1 | cut -d: -f1)
    if [ "$e_line" -le "$b_line" ]; then
      echo "sigstore setup: malformed managed hosts block" >&2
      return 1
    fi
  fi

  # No block present: the file is already in the desired state.
  if [ "$b_count" -eq 0 ]; then
    return 0
  fi

  local tmp
  if ! tmp="$(mktemp "${TMPDIR:-/tmp}/ocm-hosts.XXXXXX")"; then
    return 1
  fi

  if ! awk \
      -v "b_mark=# BEGIN OCM SIGSTORE SCAFFOLDING" \
      -v "e_mark=# END OCM SIGSTORE SCAFFOLDING" \
      '
      BEGIN { in_block = 0 }
      $0 == b_mark { in_block = 1; next }
      $0 == e_mark { in_block = 0; next }
      !in_block { print }
      ' "$hosts_file" > "$tmp"; then
    rm -f "$tmp"
    return 1
  fi

  if ! cat "$tmp" > "$hosts_file"; then
    rm -f "$tmp"
    return 1
  fi
  rm -f "$tmp"
}
