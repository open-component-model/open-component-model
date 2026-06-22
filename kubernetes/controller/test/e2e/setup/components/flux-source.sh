#!/usr/bin/env bash
# Install Flux's source-controller.
set -euo pipefail

if kubectl get deployment source-controller -n flux-system >/dev/null 2>&1 \
   && kubectl get deployment source-controller -n flux-system -o jsonpath='{.status.availableReplicas}' | grep -q '[1-9]'; then
  echo "flux source-controller already installed and running, skipping"
  exit 0
fi

flux install --components=source-controller
