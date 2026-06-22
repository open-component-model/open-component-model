#!/usr/bin/env bash
# Compatibility shim. The real setup logic now lives in
# `test/e2e/setup/local.sh`, which dispatches per-component scripts in
# `test/e2e/setup/components/`. Keep this file so the existing Taskfile
# target (`bash test/e2e/hacks/setup.sh`) continues to work without
# changes.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec bash "${script_dir}/../setup/local.sh" "$@"
