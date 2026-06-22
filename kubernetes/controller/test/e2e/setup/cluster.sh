#!/usr/bin/env bash
# Bootstrap the local kind cluster and the host-side OCI registry the
# e2e suite pushes component versions to. This script is "always on":
# every component script in setup/components/ assumes a working kind
# cluster, and every scenario at minimum needs the registry the
# controller pulls from.
#
# Sections (each idempotent):
#   1. host prereqs         — required CLI tools must be on $PATH
#   2. host registry        — image-registry container + /etc/hosts +
#                             reachability sanity check
#   3. kind cluster         — create cluster, wire containerd, attach
#                             registry to kind network
#   4. cluster RBAC         — controller-manager role/binding the
#                             scenarios depend on
#
# DESIGN.md §"Setup composition": cluster.sh always runs; per-scenario
# `requires:` then drives setup/components/<name>.sh.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
manifests_dir="${script_dir}/manifests"

# --- 1. host prereqs --------------------------------------------------
cmds=(docker flux helm jq kind kubectl)
missing=()
for cmd in "${cmds[@]}"; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    missing+=("${cmd}")
  fi
done
if [[ ${#missing[@]} -gt 0 ]]; then
  echo "missing required command(s): ${missing[*]}" >&2
  echo "install them and re-run setup/local.sh" >&2
  exit 1
fi
echo "all required commands present: ${cmds[*]}"

# --- 2. host registry -------------------------------------------------
# `http://image-registry:5000` is the registry the e2e suite pushes to
# and the controller pulls from. On macOS, AirPlay Receiver binds *:5000
# via ControlCenter — when the docker container is missing every push
# 403s as `Server: AirTunes`. We enforce container presence and
# reachability before the kind cluster is created.
reg_name='image-registry'
reg_port='5000'

if [[ "$(docker inspect -f '{{.State.Running}}' "${reg_name}" 2>/dev/null || true)" != 'true' ]]; then
  echo "starting ${reg_name} container"
  docker run \
    -d --restart=always \
    -p "127.0.0.1:${reg_port}:${reg_port}" \
    --network bridge \
    --name "${reg_name}" \
    registry:2 >/dev/null
else
  echo "${reg_name} container already running"
fi

if [[ ! -f /etc/hosts ]]; then
  echo "no /etc/hosts file present" >&2
  exit 1
fi
if ! grep -q "[[:space:]]${reg_name}\b" /etc/hosts; then
  echo "adding '127.0.0.1 ${reg_name}' to /etc/hosts (sudo required)"
  echo "127.0.0.1 ${reg_name}" | sudo tee -a /etc/hosts >/dev/null
else
  echo "/etc/hosts already maps ${reg_name}"
fi

echo "verifying registry is reachable on 127.0.0.1:${reg_port}"
status="$(curl -sI -o /dev/null -w '%{http_code}' "http://127.0.0.1:${reg_port}/v2/" || true)"
if [[ "${status}" != "200" ]]; then
  echo "registry health check failed: HTTP ${status:-no-response} from http://127.0.0.1:${reg_port}/v2/" >&2
  echo "if this is macOS, check that nothing else is bound to *:5000:" >&2
  echo "  lsof -i :${reg_port} -P" >&2
  exit 1
fi
status_via_alias="$(curl -sI -o /dev/null -w '%{http_code}' "http://${reg_name}:${reg_port}/v2/" || true)"
if [[ "${status_via_alias}" != "200" ]]; then
  echo "registry health check via ${reg_name}:${reg_port} failed: HTTP ${status_via_alias:-no-response}" >&2
  exit 1
fi
echo "${reg_name}:${reg_port} healthy"

# --- 3. kind cluster --------------------------------------------------
KIND_NODE_IMAGE_VERSION="${KIND_NODE_IMAGE_VERSION:?must be set, e.g. 1.35.1}"
KIND_NODE_IMAGE="kindest/node:v${KIND_NODE_IMAGE_VERSION}"

if kind get clusters | grep -q '^kind$'; then
  echo "kind cluster already exists, skipping create"
else
  echo "creating kind cluster (${KIND_NODE_IMAGE})"
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
   config_path = "/etc/containerd/certs.d"
EOF
fi

add_hosts_toml() {
  local node="$1" path="$2" host="$3"
  docker exec "${node}" mkdir -p "${path}"
  cat <<EOF | docker exec -i "${node}" cp /dev/stdin "${path}/hosts.toml"
[host."${host}"]
  skip_verify = true
EOF
}

CONTAINERD_CONFIG_PATH="/etc/containerd/certs.d"
for node in $(kind get nodes); do
  add_hosts_toml "${node}" "${CONTAINERD_CONFIG_PATH}/${reg_name}:${reg_port}" "http://${reg_name}:${reg_port}"
  add_hosts_toml "${node}" "${CONTAINERD_CONFIG_PATH}/localhost:31002"        "registry-internal.default.svc.cluster.local:5002"
  add_hosts_toml "${node}" "${CONTAINERD_CONFIG_PATH}/localhost:31003"        "registry-internal.default.svc.cluster.local:5003"
done

if [[ "$(docker inspect -f '{{json .NetworkSettings.Networks.kind}}' "${reg_name}")" = 'null' ]]; then
  echo "connecting ${reg_name} to kind network"
  docker network connect kind "${reg_name}"
else
  echo "${reg_name} already on kind network"
fi

# --- 4. cluster RBAC --------------------------------------------------
rbac="${manifests_dir}/rbac.yaml"
if [[ ! -f "${rbac}" ]]; then
  echo "missing manifest: ${rbac}" >&2
  exit 1
fi
kubectl apply -f "${rbac}"

echo
echo "cluster.sh complete."
