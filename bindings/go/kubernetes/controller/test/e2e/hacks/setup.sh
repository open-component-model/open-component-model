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
if kind get clusters | grep -q "^ocm-e2e$"; then
  echo "Kind cluster 'ocm-e2e' is already running. Please delete it before running this script."
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
reg_name='ocm-e2e-image-registry'
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
cat <<EOF | kind create cluster --name ocm-e2e --image="${KIND_NODE_IMAGE}" --config=-
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

for node in $(kind get nodes --name ocm-e2e); do
  add_hosts_toml "${node}" "${CONTAINERD_CONFIG_PATH}/${reg_name}:${reg_port}" "http://${reg_name}:${reg_port}"
  add_hosts_toml "${node}" "${CONTAINERD_CONFIG_PATH}/localhost:31002" "registry-internal.default.svc.cluster.local:5002"
  add_hosts_toml "${node}" "${CONTAINERD_CONFIG_PATH}/localhost:31003" "registry-internal.default.svc.cluster.local:5003"
done

# Connect the registry to the cluster network if not already connected.
## This allows kind to bootstrap the network but ensures they're on the same network.
if [ "$(docker inspect -f='{{json .NetworkSettings.Networks.kind}}' "${reg_name}")" = 'null' ]; then
  docker network connect --alias image-registry "kind" "${reg_name}"
fi

# Make sure the image registry is resolvable using localhost
if [[ ! -f /etc/hosts ]]; then
  echo "No /etc/hosts file found. Required for localhost resolution to address the image registry."
  exit 1
fi

if ! grep -q ${reg_name} /etc/hosts; then
  echo "adding '127.0.0.1 ${reg_name}' to /etc/hosts"
  echo "127.0.0.1 ${reg_name}" | sudo tee -a /etc/hosts
fi
# Create private image registries in cluster
# Fan out the independent post-cluster installs (image registries + RBAC,
# Flux, ArgoCD, KRO) as background jobs, then join on a single barrier so
# any failure fails the script and dependent steps (adding the ArgoCD
# Helm-OCI repo-creds secret) run only after their prerequisite completes.
run_step() {
  local label="$1"; shift
  local log
  log=$(mktemp)
  if "$@" > "$log" 2>&1; then
    echo "[OK] ${label}"
    rm -f "${log}"
  else
    local ec=$?
    echo "[FAIL] ${label} (exit ${ec})" >&2
    sed 's/^/  | /' "${log}" >&2
    rm -f "${log}"
    return "${ec}"
  fi
}

install_registries() {
  kubectl apply -f "${image_registries}" || return 1
  kubectl apply -f "${rbac}" || return 1
  kubectl wait pod -l app=protected-registry1 --for condition=Ready --timeout 5m || return 1
  kubectl wait pod -l app=protected-registry2 --for condition=Ready --timeout 5m || return 1
}

install_flux() {
  flux install
}

install_argocd() {
  # Install argo cd
  # If needed, overwrite the ARGOCD_VERSION. We install stable to make sure that our deployment
  # always works with the latest argo installation.
  argocd_version="${ARGOCD_VERSION:-stable}"

  kubectl get namespace argocd &>/dev/null || kubectl create namespace argocd
  kubectl apply -n argocd --server-side --force-conflicts -f "https://raw.githubusercontent.com/argoproj/argo-cd/${argocd_version}/manifests/install.yaml" || return 1

  kubectl wait -n argocd deployment \
      argocd-server \
      argocd-repo-server \
      argocd-redis \
      argocd-dex-server \
      argocd-applicationset-controller \
      argocd-notifications-controller \
      --for=condition=Available --timeout=5m || return 1

  # Widen argocd-repo-server's OCI layer mediaType allowlist to include
  # flux-native artifacts (application/vnd.cncf.flux.content.v1.tar+gzip),
  # produced from ./kustomize by test/utils.buildKustomizeOCILayout for the
  # kustomize-configuration-localization example. Defaults reproduced from
  # argocd's cmd/argocd-repo-server/commands/argocd_repo_server.go.
  kubectl -n argocd set env deploy/argocd-repo-server \
      ARGOCD_REPO_SERVER_OCI_LAYER_MEDIA_TYPES="application/vnd.oci.image.layer.v1.tar,application/vnd.oci.image.layer.v1.tar+gzip,application/vnd.cncf.helm.chart.content.v1.tar+gzip,application/vnd.cncf.flux.content.v1.tar+gzip" || return 1
  kubectl -n argocd rollout status deploy/argocd-repo-server --timeout=2m || return 1

  # Register the local OCI registry with ArgoCD as an insecure (plain HTTP) Helm OCI
  # credential template. Any Application whose repoURL starts with oci://ocm-e2e-image-registry:5000
  # inherits these settings. insecureOCIForceHttp is required because the local registry
  # serves plain HTTP; ArgoCD otherwise defaults to HTTPS and fails the chart pull.
  # IMPORTANT: the url must use "image-registry" (the docker network alias), not the
  # container name, because that is the hostname the OCM controller resolves and
  # surfaces in resource.status.additional.registry — which kro then copies verbatim
  # into the ArgoCD Application's repoURL. The credential template only matches when
  # the url prefix is identical to the Application's repoURL.
  kubectl apply -n argocd -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: ${reg_name}-creds
  namespace: argocd
  labels:
    argocd.argoproj.io/secret-type: repo-creds
stringData:
  url: oci://${reg_name}:${reg_port}
  type: helm
  enableOCI: "true"
  insecureOCIForceHttp: "true"
EOF
}

install_kro() {
  helm install kro oci://registry.k8s.io/kro/charts/kro --namespace kro --create-namespace --version=0.9.2
}

pids=()
run_step "image-registries"   install_registries & pids+=($!)
run_step "flux"               install_flux       & pids+=($!)
run_step "argocd"             install_argocd     & pids+=($!)
run_step "kro"                install_kro        & pids+=($!)

fail=0
for pid in "${pids[@]}"; do
  wait "${pid}" || fail=1
done
if [ "${fail}" -ne 0 ]; then
  echo "one or more setup steps failed; see logs above" >&2
  exit 1
fi
