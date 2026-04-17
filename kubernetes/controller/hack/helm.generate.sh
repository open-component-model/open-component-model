#!/usr/bin/env bash
#
# helm.generate.sh — post-process controller-gen CRD output into Helm chart
# templates. Takes the raw CRD files from <src-dir>, reformats them to
# 4-space indent, injects the Helm template wrappers that the chart needs
# (crd.enable toggle, cert-manager CA injection annotation, conversion
# webhook block gated on webhook.enable), and writes them to
# <chart-dir>/templates/crd/<plural>.<group>.yaml.
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
    out="${TARGET_DIR}/${plural}.${group}.yaml"

    tmp=$(mktemp)
    "${YQ}" -I4 -P eval '.' "${src}" > "${tmp}"

    awk '
        NR == 1 && /^---$/ {
            print "{{- if .Values.crd.enable }}"
            next
        }
        /^        controller-gen\.kubebuilder\.io\/version:/ {
            print
            print "        {{- if and .Values.webhook.enable .Values.certManager.enable }}"
            print "        cert-manager.io/inject-ca-from: {{ .Release.Namespace }}/{{ include \"ocm-k8s-toolkit.resourceName\" (dict \"suffix\" \"serving-cert\" \"context\" $) }}"
            print "        {{- end }}"
            next
        }
        /^spec:$/ {
            print
            print "    {{- if .Values.webhook.enable }}"
            print "    conversion:"
            print "        strategy: Webhook"
            print "        webhook:"
            print "            clientConfig:"
            print "                service:"
            print "                    name: {{ include \"ocm-k8s-toolkit.resourceName\" (dict \"suffix\" \"webhook-service\" \"context\" $) }}"
            print "                    namespace: {{ .Release.Namespace }}"
            print "                    path: /convert"
            print "            conversionReviewVersions:"
            print "                - v1"
            print "    {{- end }}"
            next
        }
        { print }
        END { print "{{- end }}" }
    ' "${tmp}" > "${out}"

    rm "${tmp}"
done
