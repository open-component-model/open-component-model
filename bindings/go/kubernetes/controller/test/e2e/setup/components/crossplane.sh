#!/usr/bin/env bash
# Install Crossplane, function-patch-and-transform, provider-kubernetes (in-cluster), and its ProviderConfig.
set -euo pipefail

CROSSPLANE_VERSION="${CROSSPLANE_VERSION:-2.3.1}"
PROVIDER_KUBERNETES_VERSION="${PROVIDER_KUBERNETES_VERSION:-v1.2.1}"
FUNCTION_P_AND_T_VERSION="${FUNCTION_P_AND_T_VERSION:-v0.10.6}"

if kubectl get deployment crossplane -n crossplane-system >/dev/null 2>&1 \
   && kubectl get deployment crossplane -n crossplane-system -o jsonpath='{.status.availableReplicas}' | grep -q '[1-9]'; then
  echo "crossplane already installed and running, skipping"
else
  helm repo add crossplane-stable https://charts.crossplane.io/stable
  helm repo update crossplane-stable

  helm upgrade --install crossplane crossplane-stable/crossplane \
    --namespace crossplane-system \
    --create-namespace \
    --version "${CROSSPLANE_VERSION}" \
    --wait
fi

# Install function-patch-and-transform (required for Pipeline mode Compositions).
if kubectl get functions.pkg.crossplane.io crossplane-contrib-function-patch-and-transform >/dev/null 2>&1; then
  echo "function-patch-and-transform already installed, skipping"
else
  kubectl apply -f - <<EOF || return 1
apiVersion: pkg.crossplane.io/v1beta1
kind: Function
metadata:
  name: crossplane-contrib-function-patch-and-transform
spec:
  package: xpkg.upbound.io/crossplane-contrib/function-patch-and-transform:${FUNCTION_P_AND_T_VERSION}
EOF
fi
# Always wait — covers fresh installs and pre-existing objects that may be unhealthy
kubectl wait functions.pkg.crossplane.io/crossplane-contrib-function-patch-and-transform \
  --for=condition=Installed=True \
  --timeout=120s || return 1
kubectl wait functions.pkg.crossplane.io/crossplane-contrib-function-patch-and-transform \
  --for=condition=Healthy=True \
  --timeout=120s || return 1

# Install provider-kubernetes for in-cluster Object management.
if kubectl get providers.pkg.crossplane.io crossplane-contrib-provider-kubernetes >/dev/null 2>&1; then
  echo "provider-kubernetes already installed, skipping"
else
  kubectl apply -f - <<EOF
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: crossplane-contrib-provider-kubernetes
spec:
  package: xpkg.upbound.io/crossplane-contrib/provider-kubernetes:${PROVIDER_KUBERNETES_VERSION}
EOF

  kubectl wait providers.pkg.crossplane.io/crossplane-contrib-provider-kubernetes \
    --for=condition=Installed=True \
    --timeout=120s
  kubectl wait providers.pkg.crossplane.io/crossplane-contrib-provider-kubernetes \
    --for=condition=Healthy=True \
    --timeout=120s
fi

# Grant provider-kubernetes SA cluster-admin so it can manage OCM, Flux, etc.
# The SA name is dynamic: crossplane-contrib-provider-kubernetes-<revisionhash>
# Wait up to 60s for Crossplane to create the revision SA after the provider is Healthy.
PROVIDER_K8S_SA=""
for i in $(seq 1 12); do
  PROVIDER_K8S_SA=$(kubectl get sa -n crossplane-system --no-headers \
    -o custom-columns=NAME:.metadata.name 2>/dev/null \
    | grep '^crossplane-contrib-provider-kubernetes-' | head -1 || true)
  [ -n "$PROVIDER_K8S_SA" ] && break
  echo "waiting for provider-kubernetes SA to appear (attempt ${i}/12)..."
  sleep 5
done
if [ -z "$PROVIDER_K8S_SA" ]; then
  echo "ERROR: could not find crossplane provider-kubernetes service account after 60s" >&2
  exit 1
fi
echo "Binding cluster-admin to provider-kubernetes SA: ${PROVIDER_K8S_SA}"
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: crossplane-provider-kubernetes-admin
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: ServiceAccount
    name: ${PROVIDER_K8S_SA}
    namespace: crossplane-system
EOF

# Configure in-cluster credentials for provider-kubernetes.
kubectl apply -f - <<EOF
apiVersion: kubernetes.crossplane.io/v1alpha1
kind: ProviderConfig
metadata:
  name: kubernetes-provider
spec:
  credentials:
    source: InjectedIdentity
EOF

# Grant the OCM controller manager permission to manage Crossplane XRDs and
# Compositions so the Deployer's ApplySet conflict-detection can list them.
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: controller-manager-crossplane-e2e
rules:
  - apiGroups:
      - apiextensions.crossplane.io
    resources:
      - compositeresourcedefinitions
      - compositions
    verbs:
      - create
      - delete
      - get
      - list
      - patch
      - update
      - watch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: controller-manager-crossplane-e2e
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: controller-manager-crossplane-e2e
subjects:
  - kind: ServiceAccount
    name: ocm-k8s-toolkit-controller-manager
    namespace: ocm-k8s-toolkit-system
EOF

FUNCTION_KRO_VERSION="${FUNCTION_KRO_VERSION:-v0.2.1}"
FUNCTION_AUTO_READY_VERSION="${FUNCTION_AUTO_READY_VERSION:-v0.6.5}"

# Install function-kro (CEL-based resource graph composition).
if kubectl get functions.pkg.crossplane.io crossplane-contrib-function-kro >/dev/null 2>&1; then
  echo "function-kro already installed, skipping"
else
  kubectl apply -f - <<EOF || return 1
apiVersion: pkg.crossplane.io/v1
kind: Function
metadata:
  name: crossplane-contrib-function-kro
spec:
  package: xpkg.upbound.io/crossplane-contrib/function-kro:${FUNCTION_KRO_VERSION}
EOF
fi
# Always wait — covers fresh installs and pre-existing objects that may be unhealthy
kubectl wait functions.pkg.crossplane.io/crossplane-contrib-function-kro \
  --for=condition=Installed=True \
  --timeout=120s || return 1
kubectl wait functions.pkg.crossplane.io/crossplane-contrib-function-kro \
  --for=condition=Healthy=True \
  --timeout=120s || return 1

# Install function-auto-ready (marks XRs ready when all composed resources are ready).
if kubectl get functions.pkg.crossplane.io crossplane-contrib-function-auto-ready >/dev/null 2>&1; then
  echo "function-auto-ready already installed, skipping"
else
  kubectl apply -f - <<EOF || return 1
apiVersion: pkg.crossplane.io/v1
kind: Function
metadata:
  name: crossplane-contrib-function-auto-ready
spec:
  package: xpkg.upbound.io/crossplane-contrib/function-auto-ready:${FUNCTION_AUTO_READY_VERSION}
EOF
fi
# Always wait — covers fresh installs and pre-existing objects that may be unhealthy
kubectl wait functions.pkg.crossplane.io/crossplane-contrib-function-auto-ready \
  --for=condition=Installed=True \
  --timeout=120s || return 1
kubectl wait functions.pkg.crossplane.io/crossplane-contrib-function-auto-ready \
  --for=condition=Healthy=True \
  --timeout=120s || return 1

# Grant the Crossplane SA permission to manage OCM, Flux, and ArgoCD resources directly
# (needed when compositions create composed resources without provider-kubernetes Objects).
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: crossplane-ocm-flux-composed
rules:
  - apiGroups:
      - delivery.ocm.software
    resources:
      - resources
      - resources/status
    verbs:
      - create
      - delete
      - get
      - list
      - patch
      - update
      - watch
  - apiGroups:
      - source.toolkit.fluxcd.io
    resources:
      - ocirepositories
      - ocirepositories/status
    verbs:
      - create
      - delete
      - get
      - list
      - patch
      - update
      - watch
  - apiGroups:
      - helm.toolkit.fluxcd.io
    resources:
      - helmreleases
      - helmreleases/status
    verbs:
      - create
      - delete
      - get
      - list
      - patch
      - update
      - watch
  - apiGroups:
      - argoproj.io
    resources:
      - applications
      - applications/status
    verbs:
      - create
      - delete
      - get
      - list
      - patch
      - update
      - watch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: crossplane-ocm-flux-composed
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: crossplane-ocm-flux-composed
subjects:
  - kind: ServiceAccount
    name: crossplane
    namespace: crossplane-system
EOF
