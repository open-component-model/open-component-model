#!/bin/bash
# Creates the Kind cluster and OCI registry for the controller performance benchmark.
# Run through `task test/performance/setup`, which pins the versions below.
set -euo pipefail

: "${KIND_NODE_IMAGE_VERSION:?KIND_NODE_IMAGE_VERSION must be set}"
: "${REGISTRY_IMAGE_VERSION:?REGISTRY_IMAGE_VERSION must be set}"

cluster_name='ocm-perf'
reg_name='ocm-perf-registry'
# 5000 is avoided on purpose: macOS AirPlay Receiver listens there.
reg_port='5555'
reg_metrics_port='5556'

for cmd in docker kind kubectl; do
  if ! command -v "$cmd" &> /dev/null; then
    echo "$cmd could not be found. Please install $cmd."
    exit 1
  fi
done

if kind get clusters | grep -q "^${cluster_name}$"; then
  echo "Kind cluster '${cluster_name}' already exists. Run 'task test/performance/teardown' first."
  exit 1
fi

# The registry runs outside Kind so it does not compete with the controller for node resources.
# Its debug listener exposes Prometheus metrics, which give the OCI request counts.
if [ "$(docker inspect -f '{{.State.Running}}' "${reg_name}" 2>/dev/null || true)" != 'true' ]; then
  docker run -d --restart=always --name "${reg_name}" \
    -p "127.0.0.1:${reg_port}:5000" \
    -p "127.0.0.1:${reg_metrics_port}:5001" \
    -e REGISTRY_HTTP_DEBUG_ADDR=:5001 \
    -e REGISTRY_HTTP_DEBUG_PROMETHEUS_ENABLED=true \
    "registry:${REGISTRY_IMAGE_VERSION}"
fi

# The controller gets a dedicated, tainted worker so the API server and other
# system pods on the control plane do not skew its CPU and memory numbers.
cat <<EOF | kind create cluster --name "${cluster_name}" --image="kindest/node:v${KIND_NODE_IMAGE_VERSION}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
  - role: worker
    labels:
      ocm.software/perf: controller
EOF

kubectl --context "kind-${cluster_name}" taint nodes -l ocm.software/perf=controller \
  ocm.software/perf=controller:NoSchedule --overwrite

if [ "$(docker inspect -f='{{json .NetworkSettings.Networks.kind}}' "${reg_name}")" = 'null' ]; then
  docker network connect "kind" "${reg_name}"
fi

echo "Cluster '${cluster_name}' and registry '${reg_name}' are ready."
