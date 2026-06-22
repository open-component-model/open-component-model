#!/usr/bin/env bash
# Install Flux's kustomize-controller (+ source-controller dependency).
set -euo pipefail

if kubectl get deployment kustomize-controller -n flux-system >/dev/null 2>&1 \
   && kubectl get deployment kustomize-controller -n flux-system -o jsonpath='{.status.availableReplicas}' | grep -q '[1-9]'; then
  echo "flux kustomize-controller already installed and running, skipping"
  exit 0
fi

if kubectl get deployment source-controller -n flux-system >/dev/null 2>&1; then
  kubectl wait deployment source-controller -n flux-system \
    --for=condition=Available \
    --timeout=120s
fi

flux install --components=kustomize-controller
