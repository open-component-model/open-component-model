#!/usr/bin/env bash
#
# rbac.generate.sh — post-process controller-gen RBAC output into the Helm
# chart's manager-role.yaml. Reads <src-dir>/role.yaml, reformats to 4-space
# indent, rewrites metadata.name to the chart's templated resource name, and
# writes to <chart-dir>/templates/rbac/manager-role.yaml.
#
# The ClusterRoleBinding, ServiceAccount, leader-election Role/RoleBinding,
# and the per-kind editor/viewer aggregation roles in chart/templates/rbac/
# are NOT touched by this script — they are hand-maintained because they
# are not derivable from //+kubebuilder:rbac markers.
#
# Usage: hack/rbac.generate.sh <src-dir> <chart-dir>

set -euo pipefail

if [[ $# -ne 2 ]]; then
    echo "usage: $0 <src-dir> <chart-dir>" >&2
    exit 1
fi

SRC_DIR="${1}"
CHART_DIR="${2}"
SRC="${SRC_DIR}/role.yaml"
OUT="${CHART_DIR}/templates/rbac/manager-role.yaml"
YQ="${YQ:-yq}"

if [[ ! -f "${SRC}" ]]; then
    echo "error: source file '${SRC}' does not exist" >&2
    exit 1
fi

mkdir -p "$(dirname "${OUT}")"

NAME_SENTINEL='OCM_K8S_TOOLKIT_MANAGER_ROLE_NAME'
NAME_TPL='{{ include "ocm-k8s-toolkit.resourceName" (dict "suffix" "manager-role" "context" $) }}'

tmp=$(mktemp)
"${YQ}" -I4 -P eval ".metadata.name = \"${NAME_SENTINEL}\"" "${SRC}" > "${tmp}"

awk -v sentinel="${NAME_SENTINEL}" -v tpl="${NAME_TPL}" '
    { sub(sentinel, tpl); print }
' "${tmp}" > "${OUT}"

rm "${tmp}"
