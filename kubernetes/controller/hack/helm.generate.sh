#!/usr/bin/env bash
#
# post-process controller-gen CRD output into Helm chart
# templates. Reads raw CRDs from <src-dir>, injects the Helm wrappers that
# the chart needs (crd.enable toggle, cert-manager CA-injection annotation,
# conversion webhook block gated on webhook.enable), and writes them to
# <chart-dir>/templates/crd/<plural>.<group>.yaml.
#
# Patterns assume controller-gen's native 2-space YAML output. Controller-gen
# is pinned in .env (CONTROLLER_TOOLS_VERSION); bump there when bumping here.
#
# Usage: hack/helm.generate.sh <src-dir> <chart-dir>

set -euo pipefail

if [[ $# -ne 2 ]]; then
    echo "usage: $0 <src-dir> <chart-dir>" >&2
    exit 1
fi

SRC_DIR="${1}"
CHART_DIR="${2}"
TARGET_DIR="${CHART_DIR}/templates/crd"
# This YQ variable is overwritten in the task file with a pinned version.
YQ="${YQ:-yq}"

if [[ ! -d "${SRC_DIR}" ]]; then
    echo "error: src-dir '${SRC_DIR}' does not exist" >&2
    exit 1
fi

mkdir -p "${TARGET_DIR}"
rm -f "${TARGET_DIR}"/*.yaml

shopt -s nullglob
for src in "${SRC_DIR}"/*.yaml; do
    plural=$("${YQ}" e '.spec.names.plural' "${src}")
    group=$("${YQ}" e '.spec.group' "${src}")
    if [[ -z "${plural}" || "${plural}" == "null" || -z "${group}" || "${group}" == "null" ]]; then
        echo "error: could not extract .spec.names.plural/.spec.group from '${src}'" >&2
        exit 1
    fi
    out="${TARGET_DIR}/${plural}.${group}.yaml"

    {
        echo "{{- if .Values.crd.enable }}"
        sed -e '1{/^---$/d;}' \
            -e '/^    controller-gen\.kubebuilder\.io\/version:/a\
    {{- if and .Values.webhook.enable .Values.certManager.enable }}\
    cert-manager.io/inject-ca-from: {{ .Release.Namespace }}/{{ include "ocm-k8s-toolkit.resourceName" (dict "suffix" "serving-cert" "context" $) }}\
    {{- end }}' \
            -e '/^spec:$/a\
  {{- if .Values.webhook.enable }}\
  conversion:\
    strategy: Webhook\
    webhook:\
      clientConfig:\
        service:\
          name: {{ include "ocm-k8s-toolkit.resourceName" (dict "suffix" "webhook-service" "context" $) }}\
          namespace: {{ .Release.Namespace }}\
          path: /convert\
      conversionReviewVersions:\
        - v1\
  {{- end }}' \
            "${src}"
        echo "{{- end }}"
    } > "${out}"
done
