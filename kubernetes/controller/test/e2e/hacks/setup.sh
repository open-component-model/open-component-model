#!/bin/bash
# This is an informal setup script to create a kind cluster with the required configuration for the e2e tests.

# Requirements:
cmds=(
  docker
  flux
  helm
  jq
  kind
  kubectl
)

## Check if all required commands are available
for cmd in "${cmds[@]}"; do
  if ! command -v "$cmd" &> /dev/null; then
    echo "$cmd could not be found. Please install $cmd."
    exit 1
  fi
done

## Check that there is not a kind cluster already running
if kind get clusters | grep -q "^kind$"; then
  echo "A kind cluster is already running. Please delete it before running this script."
  exit 1
fi

script_dir="$(dirname "$0")"
image_registries="${script_dir}/image-registries.yaml"
if [ ! -f "${image_registries}" ]; then
  echo "Image registry config file not found: ${image_registries}"
  exit 1
fi

script_dir="$(dirname "$0")"
rbac="${script_dir}/rbac.yaml"
if [ ! -f "${rbac}" ]; then
  echo "RBAC testing config file not found: ${rbac}"
  exit 1
fi

# Create registry container unless it already exists
## Required to store the controller image and have a registry to transfer OCM component versions to test localization.
reg_name='image-registry'
reg_port='5000'
if [ "$(docker inspect -f '{{.State.Running}}' "${reg_name}" 2>/dev/null || true)" != 'true' ]; then
  docker run \
    -d --restart=always -p "127.0.0.1:${reg_port}:5000" --network bridge --name "${reg_name}" \
    registry:2
fi

KIND_NODE_IMAGE="kindest/node:v${KIND_NODE_IMAGE_VERSION}"

# Create kind cluster with
# - Port mappings for additional cluster OCI registries (replication tests).
# - Containerd config patches to add registry mirrors and configs for the internal registries.
# - http-alias and insecure_skip_verify.
CONTAINERD_CONFIG_PATH="/etc/containerd/certs.d"
cat <<EOF | kind create cluster --image="${KIND_NODE_IMAGE}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 31002
        hostPort: 31002
      - containerPort: 31003
        hostPort: 31003
  - role: worker
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

for node in $(kind get nodes); do
  add_hosts_toml "${node}" "${CONTAINERD_CONFIG_PATH}/${reg_name}:${reg_port}" "http://${reg_name}:${reg_port}"
  add_hosts_toml "${node}" "${CONTAINERD_CONFIG_PATH}/localhost:31002" "registry-internal.default.svc.cluster.local:5002"
  add_hosts_toml "${node}" "${CONTAINERD_CONFIG_PATH}/localhost:31003" "registry-internal.default.svc.cluster.local:5003"
done

# Connect the registry to the cluster network if not already connected.
## This allows kind to bootstrap the network but ensures they're on the same network.
if [ "$(docker inspect -f='{{json .NetworkSettings.Networks.kind}}' "${reg_name}")" = 'null' ]; then
  docker network connect "kind" "${reg_name}"
fi

# Make sure the image registry is resolvable using localhost
if [[ ! -f /etc/hosts ]]; then
  echo "No /etc/hosts file found. Required for localhost resolution to address the image registry."
  exit 1
fi

if ! grep -q "${reg_name}" /etc/hosts; then
  echo "adding '127.0.0.1 ${reg_name}' to /etc/hosts"
  echo "127.0.0.1 ${reg_name}" | sudo tee -a /etc/hosts
fi

# Create private image registries in cluster
kubectl apply -f "${image_registries}" || exit 1
kubectl apply -f "${rbac}" || exit 1
kubectl wait pod -l app=protected-registry1 --for condition=Ready --timeout 5m || exit 1
kubectl wait pod -l app=protected-registry2 --for condition=Ready --timeout 5m || exit 1

# Install flux operators
flux install || exit 1

# Install kro operators
helm install kro oci://registry.k8s.io/kro/charts/kro --namespace kro --create-namespace --version=0.8.1 || exit 1