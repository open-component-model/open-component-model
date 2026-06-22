#!/usr/bin/env bash
# Install the in-cluster password-protected registry used by the
# credentials/docker-config-json scenario.
set -euo pipefail

if kubectl get pod -l app=protected-registry2 -o jsonpath='{.items[0].status.phase}' 2>/dev/null | grep -q Running; then
  echo "protected-registry-docker-config-json already running, skipping"
  exit 0
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
manifests_dir="$(cd "${script_dir}/../manifests" && pwd)"
manifest="${manifests_dir}/protected-registry-docker-config-json.yaml"

if [[ ! -f "${manifest}" ]]; then
  echo "missing manifest: ${manifest}" >&2
  exit 1
fi

kubectl apply -f "${manifest}"
kubectl wait pod -l app=protected-registry2 --for=condition=Ready --timeout=5m

echo "waiting for NodePort 31003 to become reachable..."
for i in $(seq 1 30); do
  if curl -sf -o /dev/null -u admin2:admin2 http://localhost:31003/v2/ 2>/dev/null; then
    echo "NodePort 31003 reachable"
    break
  fi
  sleep 1
done
