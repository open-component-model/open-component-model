# Sovereign Scenario Usage Guide

## Quick Start

Run the complete sovereign scenario end-to-end:

```bash
task run
```

This single command checks prerequisites, sets up a Kind cluster with a local registry, builds and signs all OCM components, transfers them through a simulated air-gap, imports them into the cluster registry, and bootstraps the deployment.

### Prerequisites

The following tools must be available on your PATH:

- `docker` — container runtime and buildx
- `kubectl` — Kubernetes CLI
- `kind` — local Kubernetes clusters
- `helm` — Kubernetes package manager
- `flux` — FluxCD CLI
- `openssl` — RSA key generation

Run `task check` to verify all prerequisites are installed.

## Configuration Options

Override variables on the command line (e.g., `VERSION=2.0.0 task run`):

| Variable           | Default                                                        | Description                                                      |
|--------------------|----------------------------------------------------------------|------------------------------------------------------------------|
| `VERSION`          | `1.0.0`                                                        | Component version (use `1.0.0` for initial, `1.1.0` for upgrade) |
| `POSTGRES_VERSION` | `18`                                                           | PostgreSQL image tag                                             |
| `CLUSTER_NAME`     | `sovereign-conformance`                                        | Kind cluster name                                                |
| `REGISTRY_NAME`    | `registry`                                                     | Local registry container name                                    |
| `REGISTRY_PORT`    | `5001`                                                         | Local registry host port                                         |
| `PLATFORMS`        | `linux/amd64`                                                  | Docker buildx target platform                                    |
| `KRO_VERSION`      | `0.9.0`                                                        | kro Helm chart version                                           |
| `CLI_IMAGE`        | `ghcr.io/open-component-model/cli:main`                        | OCM CLI container image                                          |
| `TOOLKIT_IMAGE`    | `ghcr.io/open-component-model/kubernetes/controller/chart:...` | OCM toolkit Helm chart OCI reference                             |

## End-to-End Flow

`task run` executes the following stages in order:

### 1. `check` — Verify prerequisites

Confirms that `docker`, `kubectl`, `kind`, `helm`, `flux`, and `openssl` are on PATH.

### 2. `clean` — Remove prior state

Deletes the `./tmp` directory, removes any existing Kind cluster, and stops the local registry container.

### 3. `prepare` — Initialize working directory

Creates the `tmp/` directory and extracts Docker credentials from `~/.docker/config.json` into a format the containerized OCM CLI can use.

### 4. `cluster:setup` — Create cluster infrastructure

Runs four sub-tasks in sequence:

1. **`cluster:registry`** — Starts a local Docker registry container on `127.0.0.1:5001`
2. **`cluster:create`** — Creates a Kind cluster with containerd registry configuration and ingress port mappings
3. **`cluster:registry:configure`** — Configures containerd on all cluster nodes to reach the local registry and connects the registry to the Kind Docker network
4. **`cluster:install:controllers`** — Installs three controllers in parallel:
   - OCM Kubernetes Toolkit (Helm chart)
   - kro — ResourceGraphDefinition controller (Helm chart)
   - FluxCD — source-controller and helm-controller

### 5. `build:product` — Build all OCM components

Builds in parallel then assembles:

1. **`build:notes`** — Builds the notes Go application image via `docker buildx`, then adds it as an OCM component to the CTF archive
2. **`build:postgres`** — Adds the PostgreSQL OCM component (references the official postgres image) to the CTF archive
3. **`product:keys`** — Generates an RSA 4096-bit key pair (`tmp/keys/acme-private.pem`, `tmp/keys/acme-public.pem`) if not already present
4. Assembles the product meta-component referencing both sub-components
5. **`sign`** — Signs the product component with the generated RSA key

### 6. `transfer:airgap` — Simulate air-gap transfer

1. Verifies the signature on the source CTF archive
2. Copies the component with all resources into a separate air-gap CTF archive

### 7. `cluster:import` — Push into cluster registry

Transfers components from the air-gap CTF archive into the in-cluster registry (`registry:5000`).

### 8. `cluster:bootstrap` — Deploy to Kubernetes

1. Applies RBAC rules (`deploy/rbac.yaml`)
2. Creates the `sovereign-product` namespace (`deploy/namespace.yaml`)
3. Creates a Kubernetes secret with the public signing key
4. Applies OCM bootstrap resources — Repository, Component, Resource, and Deployer CRs (`deploy/bootstrap.yaml`)
5. Waits for the Deployer and ResourceGraphDefinition to become Ready
6. Applies the SovereignProduct custom resource (`deploy/sample-product-1.0.0.yaml`)
7. Waits for the SovereignProduct to become Ready

## Step-by-Step Testing

Run each stage independently to test incrementally:

```bash
task check
task clean
task prepare
task cluster:setup
task build:product
task transfer:airgap
task cluster:import
task cluster:bootstrap
task verify:deployment
```

## Upgrade Workflow

After a successful initial deployment (`task run`), test the upgrade path:

```bash
task upgrade
```

### What changes between v1.0.0 and v1.1.0

The upgrade exercises a real schema migration in the notes service:

- **v1.0.0** (`cmd/sovereign-notes-v1`) — ships an initial database schema directly with no migration tracking. The `Note` model has `id`, `content`, and `created_at` fields.
- **v1.1.0** (`cmd/sovereign-notes`) — introduces incremental migrations to evolve the schema created by v1.0.0. Adds a `title` field to the `Note` model.

The Dockerfile selects the correct entrypoint binary based on the `VERSION` build arg, so the same image build pipeline produces either version.

### What `task upgrade` does

1. **Clears archives** — removes the existing CTF (`tmp/transport-archive`) and air-gap CTF (`tmp/airgap-archive`) so components are rebuilt from scratch
2. **`build:product`** — rebuilds all three OCM components (notes, postgres, product) at version `1.1.0`, generates keys if missing, and signs the product
3. **`transfer:airgap`** — verifies the v1.1.0 signature and copies resources into a fresh air-gap archive
4. **`cluster:import`** — pushes the v1.1.0 components from the air-gap archive into the in-cluster registry
5. **Applies `deploy/sample-product-1.1.0.yaml`** — updates the SovereignProduct CR, changing `spec.version` from `1.0.0` to `1.1.0`. This triggers the OCM Controller to reconcile the new component version, which causes:
   - The Component CR to fetch the updated descriptor
   - The Resource CRs to resolve new image references
   - FluxCD to deploy the updated Helm releases
   - The notes service to perform a rolling update with the database migration
6. **Waits for readiness** — blocks until the SovereignProduct CR reports Ready (timeout: 300s)

### Prerequisites

The upgrade task assumes `task run` has already completed successfully — the Kind cluster, registry, controllers, and initial v1.0.0 deployment must all be in place.

### Verifying the upgrade

After `task upgrade` completes, verify the new version is running:

```bash
# Check that pods are running the updated image
kubectl -n sovereign-product get pods -o wide

# Test connectivity (the /notes endpoint now supports title field)
task test:connectivity

# Inspect the component version in the cluster
task status
```

## Useful Commands

### Check cluster state

```bash
task status
```

Dumps all OCM custom resources (Repositories, Components, Resources, Deployers) and a full cluster-info dump of the `sovereign-product` namespace.

### Test application connectivity

```bash
task test:connectivity
```

Port-forwards the notes service and verifies the `/readyz` and `/notes` endpoints respond successfully.

### Verify component signatures

```bash
task verify COMPONENT='tmp/transport-archive//acme.org/sovereign/product:1.0.0'
```

The `COMPONENT` variable is required and specifies the component reference to verify.

## Cleanup

```bash
task clean
```

This removes:

- `./tmp` directory (CTF archives, credentials, keys)
- The Kind cluster (`sovereign-conformance`)
- The local Docker registry container

## Troubleshooting

```bash
# Check OCM resource status
task status

# Check pod status in the product namespace
kubectl -n sovereign-product get pods -o wide
kubectl -n sovereign-product describe pods

# Check controller logs
kubectl logs -n ocm-k8s-toolkit-system -l app.kubernetes.io/name=ocm-k8s-toolkit --tail=50
kubectl logs -n kro -l app.kubernetes.io/name=kro --tail=50

# Test connectivity manually
kubectl -n sovereign-product port-forward svc/sovereign-notes 8085:80
curl http://localhost:8085/readyz
curl http://localhost:8085/notes
```
