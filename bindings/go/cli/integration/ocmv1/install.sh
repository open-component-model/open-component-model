#!/usr/bin/env bash
set -euo pipefail

version="$1"
root="$(git rev-parse --show-toplevel)"
target="$root/bindings/go/cli/tmp/ocmv1/$version"

if [ -x "$target/ocm" ]; then
  exit 0
fi

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo 'Unsupported OCM v1 CLI architecture' >&2; exit 1 ;;
esac
asset="ocm-${version#v}-${os}-${arch}.tar.gz"
url="https://github.com/open-component-model/ocm/releases/download/$version"
mkdir -p "$target"
curl -fsSL "$url/$asset" -o "$target/$asset"
curl -fsSL "$url/$asset.sha256" -o "$target/$asset.sha256"
actual="$(openssl dgst -sha256 -r "$target/$asset" | awk '{print $1}')"
expected="$(tr -d '[:space:]' < "$target/$asset.sha256")"
if [ "$actual" != "$expected" ]; then
  echo "OCM v1 CLI checksum mismatch for $asset" >&2
  exit 1
fi
tar -xzf "$target/$asset" -C "$target" ocm
