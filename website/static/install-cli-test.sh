#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
FAILURES=""

# Source the install script; the BASH_SOURCE guard prevents execution.
# Tests manage their own TMP_METADATA via local tmpdirs - do not call setup_tmp().
source "${SCRIPT_DIR}/install-cli.sh"

assert_equals() {
    local test_name="$1" expected="$2" actual="$3"
    TESTS_RUN=$((TESTS_RUN + 1))
    if [[ "${expected}" == "${actual}" ]]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        echo "  PASS: ${test_name}"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        FAILURES="${FAILURES}  FAIL: ${test_name}: expected '${expected}', got '${actual}'\n"
        echo "  FAIL: ${test_name}: expected '${expected}', got '${actual}'"
    fi
}

# ---------------------------------------------------------------------------
# Version extraction tests
# ---------------------------------------------------------------------------

test_stable_version_from_mixed_releases() {
    echo "--- stable version from mixed releases ---"
    local tmpdir
    tmpdir=$(mktemp -d)
    trap 'unset -f download 2>/dev/null; rm -r "${tmpdir}"' RETURN
    TMP_METADATA="${tmpdir}/ocm.json"

    download() { cat > "$1" <<'FIXTURE'
[
  {"tag_name": "cli/v0.4.0-rc.2", "other": "data"},
  {"tag_name": "kubernetes/controller/v0.4.0-rc.1", "other": "data"},
  {"tag_name": "cli/v0.3.0", "other": "data"},
  {"tag_name": "cli/v0.2.0", "other": "data"}
]
FIXTURE
    }

    unset OCM_VERSION 2>/dev/null || true
    get_release_version >/dev/null 2>&1
    assert_equals "picks stable 0.3.0 from mixed releases" "0.3.0" "${OCM_VERSION}"
}

# Regression test: the script previously picked the first cli/ tag regardless
# of whether it was a pre-release, causing users to download rc builds.
test_prerelease_excluded() {
    echo "--- pre-release tags excluded (regression) ---"
    local tmpdir
    tmpdir=$(mktemp -d)
    trap 'unset -f download 2>/dev/null; rm -r "${tmpdir}"' RETURN
    TMP_METADATA="${tmpdir}/ocm.json"

    download() { cat > "$1" <<'FIXTURE'
[
  {"tag_name": "cli/v0.4.0-rc.2"},
  {"tag_name": "cli/v0.4.0-rc.1"},
  {"tag_name": "cli/v0.3.0"},
  {"tag_name": "cli/v0.3.0-rc.1"}
]
FIXTURE
    }

    unset OCM_VERSION 2>/dev/null || true
    get_release_version >/dev/null 2>&1
    assert_equals "skips rc tags, picks 0.3.0" "0.3.0" "${OCM_VERSION}"
}

test_non_cli_tags_excluded() {
    echo "--- non-CLI tags excluded ---"
    local tmpdir
    tmpdir=$(mktemp -d)
    trap 'unset -f download 2>/dev/null; rm -r "${tmpdir}"' RETURN
    TMP_METADATA="${tmpdir}/ocm.json"

    download() { cat > "$1" <<'FIXTURE'
[
  {"tag_name": "kubernetes/controller/v1.0.0"},
  {"tag_name": "website/v2.0.0"},
  {"tag_name": "cli/v0.5.0"}
]
FIXTURE
    }

    unset OCM_VERSION 2>/dev/null || true
    get_release_version >/dev/null 2>&1
    assert_equals "ignores non-cli tags, picks 0.5.0" "0.5.0" "${OCM_VERSION}"
}

test_explicit_ocm_version() {
    echo "--- explicit OCM_VERSION ---"

    OCM_VERSION="1.2.3"
    get_release_version >/dev/null 2>&1
    assert_equals "uses explicit version 1.2.3" "1.2.3" "${OCM_VERSION}"
    unset OCM_VERSION
}

test_explicit_prerelease_ocm_version() {
    echo "--- explicit pre-release OCM_VERSION ---"

    OCM_VERSION="0.4.0-rc.2"
    get_release_version >/dev/null 2>&1
    assert_equals "allows explicit pre-release 0.4.0-rc.2" "0.4.0-rc.2" "${OCM_VERSION}"
    unset OCM_VERSION
}

# ---------------------------------------------------------------------------
# OS and arch detection tests
# ---------------------------------------------------------------------------

test_os_detection() {
    echo "--- OS detection ---"

    OS="Darwin"
    setup_verify_os
    assert_equals "Darwin -> darwin" "darwin" "${OS}"

    OS="Linux"
    setup_verify_os
    assert_equals "Linux -> linux" "linux" "${OS}"

    local status=0
    (OS="Windows" setup_verify_os) 2>/dev/null || status=$?
    assert_equals "Windows -> fatal exit" "1" "${status}"
}

test_arch_detection() {
    echo "--- arch detection ---"

    ARCH="x86_64"
    setup_verify_arch
    assert_equals "x86_64 -> amd64" "amd64" "${ARCH}"

    ARCH="aarch64"
    setup_verify_arch
    assert_equals "aarch64 -> arm64" "arm64" "${ARCH}"

    ARCH="arm64"
    setup_verify_arch
    assert_equals "arm64 -> arm64" "arm64" "${ARCH}"

    ARCH="armv7l"
    setup_verify_arch
    assert_equals "armv7l -> arm" "arm" "${ARCH}"

    local status=0
    (ARCH="mips" setup_verify_arch) 2>/dev/null || status=$?
    assert_equals "mips -> fatal exit" "1" "${status}"
}

# ---------------------------------------------------------------------------
# Edge cases
# ---------------------------------------------------------------------------

test_only_prereleases_fatals() {
    echo "--- only pre-releases -> fatal ---"
    local tmpdir
    tmpdir=$(mktemp -d)
    trap 'unset -f download 2>/dev/null; rm -r "${tmpdir}"' RETURN
    TMP_METADATA="${tmpdir}/ocm.json"

    download() { cat > "$1" <<'FIXTURE'
[
  {"tag_name": "cli/v0.4.0-rc.2"},
  {"tag_name": "cli/v0.4.0-rc.1"}
]
FIXTURE
    }

    unset OCM_VERSION 2>/dev/null || true
    local status=0
    (get_release_version) >/dev/null 2>&1 || status=$?
    assert_equals "only pre-releases -> exit 1" "1" "${status}"
}

test_empty_response_fatals() {
    echo "--- empty response -> fatal ---"
    local tmpdir
    tmpdir=$(mktemp -d)
    trap 'unset -f download 2>/dev/null; rm -r "${tmpdir}"' RETURN
    TMP_METADATA="${tmpdir}/ocm.json"

    download() { cat > "$1" <<'FIXTURE'
[]
FIXTURE
    }

    unset OCM_VERSION 2>/dev/null || true
    local status=0
    (get_release_version) >/dev/null 2>&1 || status=$?
    assert_equals "empty response -> exit 1" "1" "${status}"
}

test_single_stable_release() {
    echo "--- single stable release ---"
    local tmpdir
    tmpdir=$(mktemp -d)
    trap 'unset -f download 2>/dev/null; rm -r "${tmpdir}"' RETURN
    TMP_METADATA="${tmpdir}/ocm.json"

    download() { cat > "$1" <<'FIXTURE'
[
  {"tag_name": "cli/v1.0.0"}
]
FIXTURE
    }

    unset OCM_VERSION 2>/dev/null || true
    get_release_version >/dev/null 2>&1
    assert_equals "single release -> 1.0.0" "1.0.0" "${OCM_VERSION}"
}

# ---------------------------------------------------------------------------
# Pagination tests
# ---------------------------------------------------------------------------

test_pagination_finds_stable_on_second_page() {
    echo "--- pagination: stable CLI tag on page 2 ---"
    local tmpdir
    tmpdir=$(mktemp -d)
    trap 'unset -f download 2>/dev/null; rm -r "${tmpdir}"' RETURN
    TMP_METADATA="${tmpdir}/ocm.json"

    download() {
        case "$2" in
            *page=1)
                cat > "$1" <<'FIXTURE'
[
  {"tag_name": "kubernetes/controller/v1.0.0"},
  {"tag_name": "website/v2.0.0"},
  {"tag_name": "cli/v0.4.0-rc.1"}
]
FIXTURE
                ;;
            *page=2)
                cat > "$1" <<'FIXTURE'
[
  {"tag_name": "cli/v0.3.0"},
  {"tag_name": "cli/v0.2.0"}
]
FIXTURE
                ;;
            *)
                cat > "$1" <<'FIXTURE'
[]
FIXTURE
                ;;
        esac
    }

    unset OCM_VERSION 2>/dev/null || true
    get_release_version >/dev/null 2>&1
    assert_equals "finds 0.3.0 on page 2" "0.3.0" "${OCM_VERSION}"
}

test_pagination_stops_on_empty_page() {
    echo "--- pagination: empty page -> fatal ---"
    local tmpdir
    tmpdir=$(mktemp -d)
    trap 'unset -f download 2>/dev/null; rm -r "${tmpdir}"' RETURN
    TMP_METADATA="${tmpdir}/ocm.json"

    download() {
        case "$2" in
            *page=1)
                cat > "$1" <<'FIXTURE'
[
  {"tag_name": "cli/v0.4.0-rc.2"},
  {"tag_name": "cli/v0.4.0-rc.1"}
]
FIXTURE
                ;;
            *)
                cat > "$1" <<'FIXTURE'
[]
FIXTURE
                ;;
        esac
    }

    unset OCM_VERSION 2>/dev/null || true
    local status=0
    (get_release_version) >/dev/null 2>&1 || status=$?
    assert_equals "no stable tag across pages -> exit 1" "1" "${status}"
}

# ---------------------------------------------------------------------------
# Verification tests
# ---------------------------------------------------------------------------

test_verify_skips_when_gh_not_found() {
    echo "--- verify_binary skips when gh not found ---"
    local tmpdir
    tmpdir=$(mktemp -d)
    trap 'unset -f command 2>/dev/null; rm -r "${tmpdir}"' RETURN
    TMP_BIN="${tmpdir}/ocm"
    echo "fake" > "${TMP_BIN}"

    # Hide gh from PATH
    command() { return 1; }

    local status=0
    verify_binary >/dev/null 2>&1 || status=$?
    assert_equals "verify_binary returns 0 when gh missing" "0" "${status}"
}

test_verify_skips_when_gh_not_authenticated() {
    echo "--- verify_binary skips when gh not authenticated ---"
    local tmpdir
    tmpdir=$(mktemp -d)
    trap 'unset -f gh 2>/dev/null; rm -r "${tmpdir}"' RETURN
    TMP_BIN="${tmpdir}/ocm"
    echo "fake" > "${TMP_BIN}"

    # gh exists but is not authenticated
    gh() { return 1; }

    local status=0
    verify_binary >/dev/null 2>&1 || status=$?
    assert_equals "verify_binary returns 0 when gh not authenticated" "0" "${status}"
}

test_verify_skips_when_explicitly_disabled() {
    echo "--- verify_binary skips when OCM_SKIP_VERIFY=true ---"
    local tmpdir
    tmpdir=$(mktemp -d)
    trap 'unset OCM_SKIP_VERIFY 2>/dev/null; rm -r "${tmpdir}"' RETURN
    TMP_BIN="${tmpdir}/ocm"
    echo "fake" > "${TMP_BIN}"

    OCM_SKIP_VERIFY="true"
    local status=0
    verify_binary >/dev/null 2>&1 || status=$?
    assert_equals "verify_binary returns 0 when skip set" "0" "${status}"
}

# ---------------------------------------------------------------------------
# Runner
# ---------------------------------------------------------------------------

echo ""
echo "Running install-cli.sh tests..."
echo ""

test_stable_version_from_mixed_releases
test_prerelease_excluded
test_non_cli_tags_excluded
test_explicit_ocm_version
test_explicit_prerelease_ocm_version
test_os_detection
test_arch_detection
test_only_prereleases_fatals
test_empty_response_fatals
test_single_stable_release
test_pagination_finds_stable_on_second_page
test_pagination_stops_on_empty_page
test_verify_skips_when_gh_not_found
test_verify_skips_when_gh_not_authenticated
test_verify_skips_when_explicitly_disabled

echo ""
echo "========================================"
echo "Tests run: ${TESTS_RUN}"
echo "Passed:    ${TESTS_PASSED}"
echo "Failed:    ${TESTS_FAILED}"
echo "========================================"

if [[ ${TESTS_FAILED} -gt 0 ]]; then
    echo ""
    echo "Failures:"
    echo -e "${FAILURES}"
    exit 1
fi

echo ""
echo "All tests passed."
