#!/usr/bin/env bash

# This is a script to install the OCM CLI v2 by downloading the latest release from GitHub.
# https://github.com/open-component-model/open-component-model/releases

set -euo pipefail

# Default install directory per the XDG Base Directory Specification:
# https://specifications.freedesktop.org/basedir/latest/
DEFAULT_BIN_DIR="${HOME}/.local/bin"
BIN_DIR=${1:-"${DEFAULT_BIN_DIR}"}
GITHUB_REPO="open-component-model/open-component-model"
TAG_PREFIX="cli/"

usage() {
    cat <<EOF
Usage: install-cli.sh [BIN_DIR]

Install the OCM CLI v2.

Arguments:
  BIN_DIR    Installation directory (default: ~/.local/bin)

Environment variables:
  OCM_VERSION       Install a specific version (e.g., OCM_VERSION=1.0.0)
  OCM_SKIP_VERIFY   Skip attestation verification (set to "true")

Examples:
  curl -sfL https://ocm.software/install-cli.sh | bash
  curl -sfL https://ocm.software/install-cli.sh | OCM_VERSION=1.0.0 bash
  curl -sfL https://ocm.software/install-cli.sh | bash -s -- /usr/local/bin
EOF
    exit 0
}

# Helper functions for logs
info() {
    echo '[INFO] ' "$@"
}

warn() {
    echo '[WARN] ' "$@" >&2
}

fatal() {
    echo '[ERROR] ' "$@" >&2
    exit 1
}

# Set os, fatal if operating system not supported
setup_verify_os() {
    if [[ -z "${OS:-}" ]]; then
        OS=$(uname)
    fi
    case ${OS} in
        Darwin)
            OS=darwin
            ;;
        Linux)
            OS=linux
            ;;
        *)
            fatal "Unsupported operating system ${OS}"
    esac
}

# Set arch, fatal if architecture not supported
setup_verify_arch() {
    if [[ -z "${ARCH:-}" ]]; then
        ARCH=$(uname -m)
    fi
    case ${ARCH} in
        arm|armv6l|armv7l)
            ARCH=arm
            ;;
        arm64|aarch64|armv8l)
            ARCH=arm64
            ;;
        amd64)
            ARCH=amd64
            ;;
        x86_64)
            ARCH=amd64
            ;;
        *)
            fatal "Unsupported architecture ${ARCH}"
    esac
}

# Ensure the target bin directory exists
ensure_bin_dir() {
    if ! mkdir -p "${BIN_DIR}" 2>/dev/null; then
        fatal "Cannot create ${BIN_DIR}. Run with a writable directory: curl ... | bash -s -- ~/.local/bin"
    fi
}

# Check if BIN_DIR is on PATH and warn if not
ensure_path() {
    case ":${PATH}:" in
        *:"${BIN_DIR}":*)
            return 0
            ;;
    esac

    warn "${BIN_DIR} is not in your PATH."
    warn "Add it by running:"
    warn ""
    warn '  echo "export PATH=${BIN_DIR}:$PATH" >> ~/.profile && source ~/.profile'
    warn ""
}

# Verify existence of downloader executable
verify_downloader() {
    # Return failure if it doesn't exist or is no executable
    command -v "$1" > /dev/null 2>&1 || return 1
    DOWNLOADER=$1
    return 0
}

# Create temporary directory and cleanup when done
setup_tmp() {
    TMP_DIR=$(mktemp -d -t ocm-install.XXXXXXXXXX)
    TMP_METADATA="${TMP_DIR}/ocm.json"
    TMP_BIN="${TMP_DIR}/ocm"
    cleanup() {
        local code=$?
        set +e
        trap - EXIT
        rm -r "${TMP_DIR}"
        exit ${code}
    }
    trap cleanup INT EXIT
}

# Find version from Github metadata
get_release_version() {
    if [[ -n "${OCM_VERSION:-}" ]]; then
        METADATA_URL="https://api.github.com/repos/${GITHUB_REPO}/releases/tags/${TAG_PREFIX}v${OCM_VERSION}"
    else
        # Use the list endpoint so we can filter by TAG_PREFIX; /releases/latest may
        # point to a non-CLI release (e.g. a website or docs tag published more recently).
        METADATA_URL="https://api.github.com/repos/${GITHUB_REPO}/releases"
    fi

    info "Downloading metadata ${METADATA_URL}"
    download "${TMP_METADATA}" "${METADATA_URL}"

    # tag_name has the format "cli/v0.1.0" – strip the prefix and leading "v".
    # When OCM_VERSION is unset the response is a JSON array; grep the first
    # tag_name that starts with TAG_PREFIX to avoid picking a non-CLI release.
    VERSION_OCM=$(grep '"tag_name":' "${TMP_METADATA}" | grep "\"${TAG_PREFIX}v" | head -1 | sed -E "s|.*\"${TAG_PREFIX}v([^\"]+)\".*|\1|")
    if [[ -n "${VERSION_OCM}" ]]; then
        info "Using ${VERSION_OCM} as release"
    else
        fatal "Unable to determine release version"
    fi
}

# Download file from URL
download() {
    [[ $# -eq 2 ]] || fatal 'download needs exactly 2 arguments'

    case $DOWNLOADER in
        curl)
            curl -o "$1" -sfL --proto '=https' --tlsv1.2 "$2" || fatal "Download with curl failed: RC $?"
            ;;
        wget)
            wget -qO "$1" --secure-protocol=TLSv1_2 "$2" || fatal "Download with wget failed: RC $?"
            ;;
        *)
            fatal "Incorrect executable '${DOWNLOADER}'"
            ;;
    esac
}

# Download binary from Github URL
# Assets follow the naming scheme: ocm-{OS}-{ARCH} (no version, no archive)
download_binary() {
    BIN_URL="https://github.com/${GITHUB_REPO}/releases/download/${TAG_PREFIX}v${VERSION_OCM}/ocm-${OS}-${ARCH}"
    info "Downloading binary ${BIN_URL}"
    download "${TMP_BIN}" "${BIN_URL}"
}

# Verify the downloaded binary using GitHub attestations
# Requires the GitHub CLI (gh) to be installed
verify_binary() {
    # Skip verification if explicitly disabled
    if [[ "${OCM_SKIP_VERIFY:-}" == "true" ]]; then
        warn "Skipping attestation verification (OCM_SKIP_VERIFY=true)"
        return 0
    fi

    # Check if gh CLI is available
    if ! command -v gh &> /dev/null; then
        warn "GitHub CLI (gh) not found. Skipping attestation verification."
        warn "To verify the binary, install gh: https://cli.github.com/"
        warn "Or set OCM_SKIP_VERIFY=true to suppress this warning."
        return 0
    fi

    info "Verifying binary attestation..."
    if gh attestation verify "${TMP_BIN}" --repo "${GITHUB_REPO}" 2>/dev/null; then
        info "Attestation verification successful"
    else
        fatal "Attestation verification failed. The binary may have been tampered with."
    fi
}

# Setup permissions and move binary
setup_binary() {
    info "Installing ocm to ${BIN_DIR}/ocm"

    if [[ -w "${BIN_DIR}" ]]; then
        install -m 755 "${TMP_BIN}" "${BIN_DIR}/ocm"
    else
        fatal "Cannot write to ${BIN_DIR}. Run with a writable directory: curl ... | bash -s -- ~/.local/bin"
    fi
}

# Run the install process
{
    case "${1:-}" in -h|--help) usage ;; esac
    setup_verify_os
    setup_verify_arch
    verify_downloader curl || verify_downloader wget || fatal 'Can not find curl or wget for downloading files'
    setup_tmp
    get_release_version
    download_binary
    verify_binary
    ensure_bin_dir
    setup_binary
    ensure_path
    info "OCM CLI v${VERSION_OCM} installed successfully"
}
