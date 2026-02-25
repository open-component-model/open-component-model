#!/bin/bash
# Setup air-gapped kind cluster for OCM conformance testing

set -euo pipefail

CLUSTER_NAME=${1:-sovereign-conformance}
REGISTRY_NAME="registry"
REGISTRY_PORT="5001"
REGISTRY_INTERNAL_PORT="5000"
KIND_NODE_IMAGE_VERSION="${KIND_NODE_IMAGE_VERSION:-v1.31.0}"
KIND_NODE_IMAGE="kindest/node:${KIND_NODE_IMAGE_VERSION}"
CONTAINERD_CONFIG_PATH="/etc/containerd/certs.d"

echo "Setting up air-gapped kind cluster: $CLUSTER_NAME"

# Check required dependencies
for cmd in docker kind kubectl helm flux; do
    if ! command -v $cmd >/dev/null 2>&1; then
        echo "❌ $cmd not found. Please install $cmd first."
        exit 1
    fi
done

# Create local registry if it doesn't exist
if [ "$(docker inspect -f '{{.State.Running}}' "${REGISTRY_NAME}" 2>/dev/null || true)" != 'true' ]; then
    echo "Starting local registry..."
    docker run -d \
        --restart=always \
        --name $REGISTRY_NAME \
        -p "127.0.0.1:${REGISTRY_PORT}:5000" \
        --network bridge \
        registry:2
    echo "Waiting for registry to be ready..."
    sleep 3
fi

# Create kind cluster with proper containerd config
echo "Creating kind cluster..."
cat <<EOF | kind create cluster --name $CLUSTER_NAME --image="${KIND_NODE_IMAGE}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
  extraPortMappings:
  - containerPort: 80
    hostPort: 8080
    protocol: TCP
  - containerPort: 443
    hostPort: 8443
    protocol: TCP
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry]
    config_path = "${CONTAINERD_CONFIG_PATH}"
EOF

# Add registry configs to nodes
add_hosts_toml() {
  local node="$1" path="$2" host="$3"
  docker exec "${node}" mkdir -p "${path}"
  cat <<EOF | docker exec -i "${node}" cp /dev/stdin "${path}/hosts.toml"
[host."${host}"]
  skip_verify = true
EOF
}

echo "Configuring registry access for cluster nodes..."
for node in $(kind get nodes --name $CLUSTER_NAME); do
  add_hosts_toml "${node}" "${CONTAINERD_CONFIG_PATH}/${REGISTRY_NAME}:${REGISTRY_INTERNAL_PORT}" "http://${REGISTRY_NAME}:${REGISTRY_INTERNAL_PORT}"
done

# Connect registry to kind network if not already connected
echo "Connecting registry to kind network..."
if [ "$(docker inspect -f='{{json .NetworkSettings.Networks.kind}}' "${REGISTRY_NAME}")" = 'null' ]; then
  docker network connect --alias "${REGISTRY_NAME}" "kind" "${REGISTRY_NAME}"
fi

# Install kro (ResourceGraphDefinition controller)
echo "Installing kro..."
helm install kro oci://registry.k8s.io/kro/charts/kro --namespace kro --create-namespace --version=0.8.5 || exit 1

# Install Flux
echo "Installing Flux..."
flux install --components=source-controller,helm-controller
kubectl -n flux-system wait --for=condition=Available deployment/source-controller --timeout=120s
kubectl -n flux-system wait --for=condition=Available deployment/helm-controller --timeout=120s

echo "✅ Air-gapped kind cluster $CLUSTER_NAME ready"
echo "Registry available at: localhost:$REGISTRY_PORT"

# Clean up
rm -f /tmp/kind-config.yaml