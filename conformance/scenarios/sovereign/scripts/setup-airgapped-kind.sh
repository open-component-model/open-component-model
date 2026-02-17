#!/bin/bash
# Setup air-gapped kind cluster for OCM conformance testing

set -euo pipefail

CLUSTER_NAME=${1:-sovereign-conformance}
REGISTRY_NAME="registry"
REGISTRY_PORT="5001"

echo "Setting up air-gapped kind cluster: $CLUSTER_NAME"

# Check required dependencies
for cmd in docker kind kubectl; do
    if ! command -v $cmd >/dev/null 2>&1; then
        echo "❌ $cmd not found. Please install $cmd first."
        exit 1
    fi
done

# Create local registry if it doesn't exist
if ! docker ps | grep -q $REGISTRY_NAME; then
    echo "Starting local registry..."
    docker run -d \
        --restart=always \
        --name $REGISTRY_NAME \
        -p ${REGISTRY_PORT}:5000 \
        registry:2
fi

# Create kind cluster config
cat <<EOF > /tmp/kind-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry]
    config_path = "/etc/containerd/certs.d"
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:${REGISTRY_PORT}"]
    endpoint = ["http://registry:5000"]
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
EOF

# Create kind cluster
echo "Creating kind cluster..."
kind create cluster --name $CLUSTER_NAME --config /tmp/kind-config.yaml

# Connect registry to kind network
echo "Connecting registry to kind network..."
docker network connect kind $REGISTRY_NAME || true

# Install kro (ResourceGraphDefinition controller)
echo "Installing kro..."
kubectl apply -f https://github.com/GoogleCloudPlatform/kro/releases/latest/download/kro.yaml
kubectl -n kro-system wait --for=condition=Available deployment/kro-controller --timeout=120s

# Install Flux
echo "Installing Flux..."
if ! command -v flux >/dev/null 2>&1; then
    echo "❌ flux CLI not found. Please install flux CLI first."
    echo "Installation instructions: https://fluxcd.io/flux/installation/"
    exit 1
fi
flux install --components=source-controller,helm-controller
kubectl -n flux-system wait --for=condition=Available deployment/source-controller --timeout=120s
kubectl -n flux-system wait --for=condition=Available deployment/helm-controller --timeout=120s

echo "✅ Air-gapped kind cluster $CLUSTER_NAME ready"
echo "Registry available at: localhost:$REGISTRY_PORT"

# Clean up
rm -f /tmp/kind-config.yaml