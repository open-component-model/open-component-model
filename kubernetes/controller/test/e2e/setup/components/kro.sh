#!/usr/bin/env bash
# Install the kro operator via its upstream Helm chart.
set -euo pipefail

KRO_VERSION="${KRO_VERSION:-0.9.0}"

if kubectl get deployment kro -n kro >/dev/null 2>&1 \
   && kubectl get deployment kro -n kro -o jsonpath='{.status.availableReplicas}' | grep -q '[1-9]'; then
  echo "kro already installed and running, skipping"
  exit 0
fi

helm upgrade --install kro \
  oci://registry.k8s.io/kro/charts/kro \
  --namespace kro \
  --create-namespace \
  --version "${KRO_VERSION}"
