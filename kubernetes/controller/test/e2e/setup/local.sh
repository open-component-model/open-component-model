#!/usr/bin/env bash
# Orchestrator for the local e2e environment.
#
# By default, only provisions the kind cluster + host registry (cluster.sh).
# Component installation is deferred to the e2e runner, which installs each
# scenario's dependencies on demand via `requires:`.
#
# Flags:
#   --all-components   Also install all component scripts under components/.
#                      Use this for fast local iteration when you want every
#                      component pre-installed so focused runs start instantly.
#
# DESIGN.md §"Setup composition" describes the contract:
#   * cluster.sh provisions the kind cluster + host registry + RBAC and
#     is always required.
#   * Every file in components/ corresponds to an opt-in component a
#     scenario can declare via `requires:` in its e2e.yaml. The runner
#     installs them on demand; pass --all-components to pre-install all.
set -euo pipefail

all_components=false
for arg in "$@"; do
  case "$arg" in
    --all-components) all_components=true ;;
  esac
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
components_dir="${script_dir}/components"
cluster_script="${script_dir}/cluster.sh"

if [[ ! -f "${cluster_script}" ]]; then
  echo "cluster bootstrap script not found: ${cluster_script}" >&2
  exit 1
fi

echo ">>> cluster.sh"
bash "${cluster_script}"

if [[ "${all_components}" != "true" ]]; then
  echo
  echo "setup/local.sh complete (cluster only — components installed on demand by the runner)."
  exit 0
fi

if [[ ! -d "${components_dir}" ]]; then
  echo "components directory not found: ${components_dir}" >&2
  exit 1
fi

shopt -s nullglob
scripts=("${components_dir}"/*.sh)
shopt -u nullglob

if [[ ${#scripts[@]} -eq 0 ]]; then
  echo "no component scripts found in ${components_dir}" >&2
  exit 1
fi

pids=()
tmpfiles=()
names=()
trap 'rm -f "${tmpfiles[@]}"' EXIT

for s in "${scripts[@]}"; do
  tmp=$(mktemp)
  bash "${s}" >"${tmp}" 2>&1 &
  pids+=($!)
  tmpfiles+=("${tmp}")
  names+=("$(basename "${s}")")
  echo ">>> components/$(basename "${s}") (pid $!)"
done

failed_idx=""
for i in "${!pids[@]}"; do
  if ! wait "${pids[$i]}"; then
    failed_idx="${i}"
    break
  fi
done

if [[ -n "${failed_idx}" ]]; then
  for j in "${!pids[@]}"; do
    if [[ "${j}" != "${failed_idx}" ]]; then
      kill "${pids[$j]}" 2>/dev/null || true
    fi
  done
  echo >&2
  echo ">>> components/${names[$failed_idx]} FAILED — output:" >&2
  cat "${tmpfiles[$failed_idx]}" >&2
  exit 1
fi

echo
echo "setup/local.sh complete (all components installed)."
