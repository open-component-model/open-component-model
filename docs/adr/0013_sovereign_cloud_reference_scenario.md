# Design: Reference Scenario — Sovereign Cloud Delivery with Open ComponentModel

* **Status**: draft
* **Deciders**: TBD
* **Date**: 2025-02-06

**Issue:** [ocm-project#842](https://github.com/open-component-model/ocm-project/issues/842)  
**Parent Epic:** [ocm-project#424 — OCM Reference Scenario / Golden Path](https://github.com/open-component-model/ocm-project/issues/424)  
**Related:** [ocm-project#843 — Document and Describe Reference Scenario in "How To's"](https://github.com/open-component-model/ocm-project/issues/843)

## Table of Contents

<!-- TOC -->
* [Design: Reference Scenario — Sovereign Cloud Delivery with Open ComponentModel](#design-reference-scenario--sovereign-cloud-delivery-with-open-componentmodel)
  * [Table of Contents](#table-of-contents)
  * [1. Overview](#1-overview)
    * [1.1 What This Document Covers](#11-what-this-document-covers)
    * [1.2 How to Read This Document](#12-how-to-read-this-document)
    * [1.3 Integration Landscape](#13-integration-landscape)
    * [1.5 How the Integrations Work Together](#15-how-the-integrations-work-together)
    * [1.6 Prerequisites](#16-prerequisites)
  * [2. Architecture Diagram](#2-architecture-diagram)
    * [2.1 End-to-End Flow](#21-end-to-end-flow)
    * [2.2 Controller Reconciliation Detail](#22-controller-reconciliation-detail)
  * [3. Service Design](#3-service-design)
    * [3.1 sovereign-notes (Web Service)](#31-sovereign-notes-web-service)
    * [3.2 PostgreSQL (Database)](#32-postgresql-database)
  * [4. Component Modeling](#4-component-modeling)
    * [4.0 Component Structure Overview](#40-component-structure-overview)
    * [4.1 Component: `acme.org/sovereign/notes`](#41-component-acmeorgsovereignnotes)
    * [4.2 Component: `acme.org/sovereign/postgres`](#42-component-acmeorgsovereignpostgres)
    * [4.3 Meta Component: `acme.org/sovereign/product`](#43-meta-component-acmeorgsovereignproduct)
    * [4.4 Settings File](#44-settings-file)
  * [5. Signing Workflow](#5-signing-workflow)
    * [5.1 Key Generation (One-time Setup)](#51-key-generation-one-time-setup)
    * [5.2 Sign During Build](#52-sign-during-build)
    * [5.3 Verify Before Transfer](#53-verify-before-transfer)
    * [5.4 Verify on Cluster](#54-verify-on-cluster)
  * [6. Configuration via ResourceGraphDefinition](#6-configuration-via-resourcegraphdefinition)
    * [6.1 Resource with Image Extraction](#61-resource-with-image-extraction)
    * [6.2 ResourceGraphDefinition for Deployment](#62-resourcegraphdefinition-for-deployment)
    * [6.3 RGD Instance for Environment Configuration](#63-rgd-instance-for-environment-configuration)
  * [7. Build & Publish Pipeline](#7-build--publish-pipeline)
    * [7.1 Taskfile (Local Development)](#71-taskfile-local-development)
    * [7.2 GitHub Actions Pipeline](#72-github-actions-pipeline)
  * [8. OCM Controller Deployment Manifests](#8-ocm-controller-deployment-manifests)
    * [8.1 Repository](#81-repository)
    * [8.2 Components](#82-components)
    * [8.3 Resources](#83-resources)
    * [8.4 Deployers](#84-deployers)
    * [8.5 RGD Instances (Configuration)](#85-rgd-instances-configuration)
  * [9. Upgrade Scenario](#9-upgrade-scenario)
    * [9.0 Upgrade Flow Overview](#90-upgrade-flow-overview)
    * [9.1 Version Bump](#91-version-bump)
    * [9.2 Release New Version](#92-release-new-version)
    * [9.3 Automatic Controller Reconciliation](#93-automatic-controller-reconciliation)
  * [10. Repository Layout](#10-repository-layout)
  * [11. Integration Points for Upstream Testing](#11-integration-points-for-upstream-testing)
    * [Test Commands](#test-commands)
  * [11. Local Development Workflow](#11-local-development-workflow)
    * [11.1 Prerequisites](#111-prerequisites)
    * [11.2 Quick Setup](#112-quick-setup)
    * [11.3 Development Cycle](#113-development-cycle)
    * [11.4 Component Development](#114-component-development)
    * [11.5 Air-Gap Testing](#115-air-gap-testing)
  * [12. Testing and Troubleshooting](#12-testing-and-troubleshooting)
    * [12.1 Testing Strategy](#121-testing-strategy)
    * [12.2 Test Commands](#122-test-commands)
  * [13. Deployment Extensibility](#13-deployment-extensibility)
  * [14. Key Design Decisions](#14-key-design-decisions)
<!-- TOC -->

---

## 1. Overview

This document designs a reference scenario demonstrating OCM's core value proposition: **modeling, signing, transporting, and deploying a multi-service product into an air-gapped sovereign cloud environment**.

### 1.1 What This Document Covers

The scenario uses two genuinely interdependent services:

- **sovereign-notes**: A minimal Go web service that stores notes in PostgreSQL
- **PostgreSQL**: The official postgres image, deployed via manifests

Both are packaged as OCM components, signed, transferred through an air-gap via CTF, and bootstrapped on a local kind cluster using the OCM Kubernetes controllers with Flux.

**Key principles:**

- Real codependency (not contrived)
- Configuration delivered as OCM resources (not hardcoded)
- Signed components with verification on deployment
- Upgrade flow as first-class concern
- Fully reproducible on a developer laptop

### 1.2 How to Read This Document

This document is organized in layers, starting with core OCM concepts and building up to enterprise integration scenarios:

| Sections  | Focus                                              | Key Technologies                           | Audience                  |
|-----------|----------------------------------------------------|--------------------------------------------|---------------------------|
| **2-4**   | Service & Component Design: Architecture, modeling | OCM CLI, component-constructor             | All readers — start here  |
| **5**     | Security: Signing and verification workflows       | RSA/ECDSA, ocm sign/verify                 | Security Engineers        |
| **6**     | Configuration: ResourceGraphDefinitions            | **kro**, CEL expressions                   | Platform Engineers        |
| **7-9**   | Build & Deploy: Pipelines, manifests, upgrades     | GitHub Actions, **FluxCD**, OCM Controller | DevOps Engineers          |
| **10-11** | Repository layout, testing                         | Task, Go test                              | Contributors, Integrators |
| **13-14** | Extensibility & Design decisions                   | —                                          | All readers               |

**Recommended reading paths:**

- **New to OCM?** Start with sections 1-6 to understand core concepts (components, signing, kro)
- **Building a platform?** Focus on sections 6-9 (kro + Flux)
- **Evaluating for sovereign cloud?** Read sections 1-2, 5 (signing), then 13-14
- **Understanding deployment?** Sections 6 (kro RGDs), 8 (OCM Controller), and 15 (Flux extensibility)

### 1.3 Integration Landscape

This reference scenario demonstrates how OCM integrates with the broader cloud-native ecosystem. The architecture is organized into three layers:

1. **Core Service Architecture (Sections 2-11)**: The foundation — OCM components, signing, transport, and deployment via kro and Flux
2. **Deployment Infrastructure (kro + Flux)**: Bridges OCM resources to Kubernetes workloads
3. **Enterprise Integrations (Sections 12-14)**: Service discovery, multi-control-plane, and service catalog capabilities

Each integration serves a distinct purpose in the sovereign delivery model:

**Core Deployment Infrastructure:**

| Integration                        | What It Does                                                                               | Why It Matters                                                                                         |
|------------------------------------|--------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------|
| **kro (ResourceGraphDefinitions)** | Defines deployment templates with CEL expressions for image localization and configuration | Strongly-typed configuration schemas; environment-specific values without changing component structure |
| **FluxCD**                         | GitOps-based continuous delivery; reconciles Helm releases and Kustomizations              | Mature, production-ready deployment; automatic drift detection and remediation                         |
| **OCM Controller**                 | Reconciles OCM CRs (Repository, Component, Resource, Deployer) to Kubernetes               | Bridges OCM artifacts to deployment infrastructure; handles signature verification                     |

**Enterprise Integrations:**

| Integration                       | What It Does                                                                                     | Why It Matters                                                                                                        |
|-----------------------------------|--------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------|
| **Open Resource Discovery (ORD)** | Enables services to self-describe their APIs, capabilities, and metadata via standard endpoints  | Consumers can discover what services offer without reading documentation; enables automated service catalogs          |
| **OpenMCP**                       | Provides multi-control-plane orchestration with Global (connected) and Local (air-gapped) planes | Separates component management from deployment execution; enables true air-gap scenarios with sovereign control       |
| **Platform Mesh**                 | Offers a KRM-based service catalog with ordering, dependencies, and multi-provider composition   | Enterprises can offer OCM components as self-service products; automatic dependency resolution simplifies consumption |
| **External Secrets Operator**     | Syncs secrets from external stores (Vault, AWS SM, Azure KV) into Kubernetes                     | Credentials never leave the secure perimeter; decouples secret management from application deployment                 |

### 1.5 How the Integrations Work Together

The integrations form a cohesive delivery pipeline:

```text
┌─────────────────────────────────────────────────────────────────────────────┐
│                           CONNECTED ENVIRONMENT                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. SERVICE DISCOVERY (ORD)                                                 │
│     └─► Services expose /.well-known/open-resource-discovery                │
│     └─► Aggregators collect metadata into searchable catalogs               │
│                                                                             │
│  2. SERVICE CATALOG (Platform Mesh)                                         │
│     └─► ServiceOfferings published from OCM components                      │
│     └─► Consumers browse catalog and create ServiceOrders                   │
│     └─► Dependencies (PostgreSQL, External Secrets) auto-resolved           │
│                                                                             │
│  3. COMPONENT MANAGEMENT (OpenMCP Global Control Plane)                     │
│     └─► OCM CRs created from ServiceOrders                                  │
│     └─► Components signed and verified                                      │
│     └─► CTF archives prepared for air-gap transfer                          │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │  AIR-GAP TRANSFER (CTF Archive)
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                          SOVEREIGN ENVIRONMENT                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  4. OCM CONTROLLER (Reconciliation)                                         │
│     └─► CTF imported and signatures verified                                │
│     └─► Repository CR validates registry connection                         │
│     └─► Component CR fetches and verifies component version                 │
│     └─► Resource CR extracts artifacts with CEL expressions                 │
│                                                                             │
│  5. KRO (ResourceGraphDefinition)                                           │
│     └─► RGD templates define deployment structure                           │
│     └─► RGD instances provide environment-specific values                   │
│     └─► CEL expressions localize image references to local registry         │
│                                                                             │
│  6. SECRET INJECTION (External Secrets Operator)                            │
│     └─► ExternalSecret CRs sync credentials from local Vault                │
│     └─► DATABASE_URL injected into sovereign-notes                          │
│                                                                             │
│  7. WORKLOAD DEPLOYMENT (FluxCD)                                            │
│     └─► Helm Controller deploys PostgreSQL chart                            │
│     └─► Kustomize Controller applies sovereign-notes manifests              │
│     └─► Automatic drift detection and reconciliation                        │
│     └─► Service ready for traffic                                           │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1.6 Prerequisites

To work through this reference scenario, you'll need:

| Tool       | Purpose                          | Installation                                                        |
|------------|----------------------------------|---------------------------------------------------------------------|
| `ocm` CLI  | Build, sign, transfer components | [ocm.software/docs/cli](https://ocm.software/docs/cli/)             |
| `kind`     | Local Kubernetes cluster         | [kind.sigs.k8s.io](https://kind.sigs.k8s.io/)                       |
| `flux` CLI | GitOps deployment                | [fluxcd.io/docs/installation](https://fluxcd.io/docs/installation/) |
| `task`     | Task runner for automation       | [taskfile.dev](https://taskfile.dev/)                               |
| Docker     | Container runtime                | [docker.com](https://docker.com/)                                   |

**Optional (for full integration testing):**

- External Secrets Operator (for secret management)
- OpenMCP installation (for multi-control-plane)
- Platform Mesh (for service catalog)

---

## 2. Architecture Diagram

### 2.1 End-to-End Flow

<details open>
<summary>Diagram: End-to-End Flow</summary>

```mermaid
flowchart TB
    subgraph connected["Connected Environment"]
        subgraph components["OCM Components"]
            notes[sovereign-notes]
            postgres[postgres]
            product[sovereign-product]
        end

        notes --> build
        postgres --> build
        product --> build

        build[ocm add cv + sign]
        build --> push[ocm transfer ctf]
        push --> ghcr[(ghcr.io)]
    end

    ghcr -->|verify + copy-resources| ctf

    ctf[[CTF Archive]]

    ctf -->|Air-Gap| airgap

    subgraph airgap["Air-Gapped Environment"]
        transfer[ocm transfer ctf]
        transfer --> registry[(Local Registry)]

        subgraph kind["kind cluster"]
            controller[OCM Controller]
            controller --> comp[Component]
            comp --> res[Resources]
            res --> deployer[Deployer]

            subgraph workloads["Workloads"]
                notes_deploy[sovereign-notes] -->|DATABASE_URL| pg_sts[PostgreSQL]
            end
        end

        registry --> controller
    end

    deployer -.-> workloads
```

</details>

### 2.2 Controller Reconciliation Detail

<details open>
<summary>Diagram: Controller Reconciliation</summary>

```mermaid
flowchart LR
    Repo[Repository] -->|validate| Registry[(Registry)]
    Comp[Component] -->|fetch + verify| Repo
    Comp --> Resource1[Resource: chart]
    Comp --> Resource2[Resource: image]
    Resource1 --> Deployer[Deployer]
    Resource2 --> Deployer
    Deployer --> RGD[ResourceGraphDefinition]
    RGD --> Instance[RGD Instance]
    Instance -->|values| HelmRelease
    HelmRelease -->|apply| K8s[Kubernetes]
```

</details>

---

## 3. Service Design

### 3.1 sovereign-notes (Web Service)

A minimal Go HTTP service (~100 LOC) that provides a notes API backed by PostgreSQL.

**Endpoints:**

- `GET /healthz` — liveness probe
- `GET /readyz` — readiness probe (checks DB connection)
- `GET /notes` — list all notes
- `POST /notes` — create a note
- `GET /notes/{id}` — get a note
- `DELETE /notes/{id}` — delete a note
- `GET /` — simple HTML UI

**Configuration (via environment):**

- `DATABASE_URL` — PostgreSQL connection string
- `PORT` — HTTP listen port (default 8080)

<details>
<summary><code>cmd/sovereign-notes/main.go</code> (sketch)</summary>

```go
// cmd/sovereign-notes/main.go (sketch)
package main

import (
    "database/sql"
    "encoding/json"
    "log"
    "net/http"
    "os"

    _ "github.com/lib/pq"
)

type Note struct {
    ID        int    `json:"id"`
    Content   string `json:"content"`
    CreatedAt string `json:"created_at"`
}

var db *sql.DB

func main() {
    var err error
    db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
    if err != nil {
        log.Fatal(err)
    }

    // Auto-create table
    db.Exec(`CREATE TABLE IF NOT EXISTS notes (
        id SERIAL PRIMARY KEY,
        content TEXT NOT NULL,
        created_at TIMESTAMP DEFAULT NOW()
    )`)

    http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    })
    http.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
        if err := db.Ping(); err != nil {
            http.Error(w, err.Error(), http.StatusServiceUnavailable)
            return
        }
        w.WriteHeader(http.StatusOK)
    })
    http.HandleFunc("/notes", handleNotes)
    http.HandleFunc("/", serveUI)

    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
    log.Printf("Listening on :%s", port)
    log.Fatal(http.ListenAndServe(":"+port, nil))
}
```

</details>

**Why a custom app:**

- Genuine PostgreSQL dependency (not contrived)
- Full control over versioning and behavior
- Demonstrates container image build in the component pipeline
- Small enough to understand in minutes
- Can be extended later (e.g., add Redis cache to show 3-tier)

### 3.2 PostgreSQL (Database)

Uses the **official `postgres:16` image** (not Bitnami). Deployed as a StatefulSet with a PVC for data persistence.

**Configuration (via environment):**

- `POSTGRES_USER`
- `POSTGRES_PASSWORD`
- `POSTGRES_DB`

---

## 4. Component Modeling

### 4.0 Component Structure Overview

<details open>
<summary>Diagram: Component Structure</summary>

```mermaid
flowchart TB
    subgraph product["acme.org/sovereign/product"]
        p_rgd[product-rgd]

        subgraph refs[componentReferences]
            ref_notes[notes]
            ref_postgres[postgres]
        end
    end

    subgraph notes["acme.org/sovereign/notes"]
        n_image[image]
        n_chart[helm-chart]
        n_rgd[rgd]
    end

    subgraph postgres["acme.org/sovereign/postgres"]
        pg_image[image]
        pg_chart[helm-chart]
        pg_rgd[rgd]
    end

    ref_notes --> notes
    ref_postgres --> postgres
```

</details>

### 4.1 Component: `acme.org/sovereign/notes`

<details>
<summary><code>components/notes/component-constructor.yaml</code></summary>

```yaml
# components/notes/component-constructor.yaml
components:
  - name: acme.org/sovereign/notes
    version: "${VERSION}"
    provider:
      name: acme.org
    resources:
      # The application container image (built from source)
      - name: image
        type: ociImage
        relation: local
        version: "${VERSION}"
        access:
          type: ociArtifact
          imageReference: acme.org/sovereign/notes:${VERSION}

      # Helm chart for deployment
      - name: helm-chart
        type: helmChart
        relation: local
        input:
          type: helm
          path: ./deploy/chart

      # ResourceGraphDefinition for kro deployment
      - name: rgd
        type: blob
        relation: local
        input:
          type: file
          path: ./deploy/rgd.yaml
          mediaType: application/vnd.cncf.kro.resourcegraphdefinition.v1+yaml
```

</details>

**ResourceGraphDefinition (`deploy/rgd.yaml`):**

<details>
<summary><code>deploy/rgd.yaml</code> — Notes RGD</summary>

```yaml
# This RGD defines the schema and templates for deploying notes
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: sovereign-notes
spec:
  schema:
    apiVersion: v1alpha1
    kind: SovereignNotes
    spec:
      releaseName: string | default="sovereign-notes"
      namespace: string | default="sovereign-product"
      replicas: integer | default=2
      databaseSecretRef: string | default="db-credentials"
  resources:
    - id: ociRepository
      template:
        apiVersion: source.toolkit.fluxcd.io/v1beta2
        kind: OCIRepository
        metadata:
          name: ${schema.spec.releaseName}
          namespace: ${schema.spec.namespace}
        spec:
          interval: 10m
          url: oci://${resourceChart.status.additional.registry}/${resourceChart.status.additional.repository}
          ref:
            tag: ${resourceChart.status.additional.tag}
    - id: helmRelease
      template:
        apiVersion: helm.toolkit.fluxcd.io/v2
        kind: HelmRelease
        metadata:
          name: ${schema.spec.releaseName}
          namespace: ${schema.spec.namespace}
        spec:
          interval: 10m
          chart:
            spec:
              chart: .
              sourceRef:
                kind: OCIRepository
                name: ${ociRepository.metadata.name}
          values:
            replicaCount: ${schema.spec.replicas}
            image:
              repository: ${resourceImage.status.additional.registry}/${resourceImage.status.additional.repository}
              tag: ${resourceImage.status.additional.tag}
            databaseSecretRef: ${schema.spec.databaseSecretRef}
```

</details>

### 4.2 Component: `acme.org/sovereign/postgres`

<details>
<summary><code>components/postgres/component-constructor.yaml</code></summary>

```yaml
# components/postgres/component-constructor.yaml
components:
  - name: acme.org/sovereign/postgres
    version: "${VERSION}"
    provider:
      name: acme.org
    resources:
      # Official PostgreSQL image
      - name: image
        type: ociImage
        version: "${POSTGRES_VERSION}"
        access:
          type: ociArtifact
          imageReference: docker.io/library/postgres:${POSTGRES_VERSION}

      # Helm chart for StatefulSet deployment
      - name: helm-chart
        type: helmChart
        relation: local
        input:
          type: helm
          path: ./deploy/chart

      # ResourceGraphDefinition for kro deployment
      - name: rgd
        type: blob
        relation: local
        input:
          type: file
          path: ./deploy/rgd.yaml
          mediaType: application/vnd.cncf.kro.resourcegraphdefinition.v1+yaml
```

</details>

**ResourceGraphDefinition (`deploy/rgd.yaml`):**

<details>
<summary><code>deploy/rgd.yaml</code> — Postgres RGD</summary>

```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: sovereign-postgres
spec:
  schema:
    apiVersion: v1alpha1
    kind: SovereignPostgres
    spec:
      releaseName: string | default="sovereign-postgres"
      namespace: string | default="sovereign-product"
      storage:
        size: string | default="1Gi"
        storageClass: string | default=""
  resources:
    - id: ociRepository
      template:
        apiVersion: source.toolkit.fluxcd.io/v1beta2
        kind: OCIRepository
        metadata:
          name: ${schema.spec.releaseName}
          namespace: ${schema.spec.namespace}
        spec:
          interval: 10m
          url: oci://${resourceChart.status.additional.registry}/${resourceChart.status.additional.repository}
          ref:
            tag: ${resourceChart.status.additional.tag}
    - id: helmRelease
      template:
        apiVersion: helm.toolkit.fluxcd.io/v2
        kind: HelmRelease
        metadata:
          name: ${schema.spec.releaseName}
          namespace: ${schema.spec.namespace}
        spec:
          interval: 10m
          chart:
            spec:
              chart: .
              sourceRef:
                kind: OCIRepository
                name: ${ociRepository.metadata.name}
          values:
            image:
              repository: ${resourceImage.status.additional.registry}/${resourceImage.status.additional.repository}
              tag: ${resourceImage.status.additional.tag}
            storage:
              size: ${schema.spec.storage.size}
              storageClass: ${schema.spec.storage.storageClass}
```

</details>

### 4.3 Meta Component: `acme.org/sovereign/product`

<details>
<summary><code>components/product/component-constructor.yaml</code></summary>

```yaml
# components/product/component-constructor.yaml
components:
  - name: acme.org/sovereign/product
    version: "${VERSION}"
    provider:
      name: acme.org

    # References to child components
    componentReferences:
      - name: notes
        componentName: acme.org/sovereign/notes
        version: "${VERSION}"
      - name: postgres
        componentName: acme.org/sovereign/postgres
        version: "${VERSION}"

    resources:
      # Product-level RGD for orchestrating deployment order
      - name: product-rgd
        type: blob
        relation: local
        input:
          type: file
          path: ./deploy/rgd.yaml
          mediaType: application/vnd.cncf.kro.resourcegraphdefinition.v1+yaml

      # Base namespace and secrets
      - name: base-manifests
        type: blob
        relation: local
        input:
          type: file
          path: ./deploy/base.yaml

    # RSAPSS signature for verification (shipped with product)
    signatures: ...
```

</details>

**Base manifests (`deploy/base.yaml`):**

<details>
<summary><code>deploy/base.yaml</code> — Namespace and Secrets</summary>

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: sovereign-product
---
# db-credentials.yaml (template - actual password injected via ExternalSecrets or sealed-secrets)
apiVersion: v1
kind: Secret
metadata:
  name: db-credentials
  namespace: sovereign-product
type: Opaque
stringData:
  POSTGRES_USER: notes
  POSTGRES_PASSWORD: "${DB_PASSWORD}"  # Injected at deploy time
  POSTGRES_DB: notes
  DATABASE_URL: "postgres://notes:${DB_PASSWORD}@postgres.sovereign-product.svc:5432/notes?sslmode=disable"
```

</details>

**Product RGD (`deploy/rgd.yaml`):**

<details>
<summary><code>deploy/rgd.yaml</code> — Product Orchestration RGD</summary>

```yaml
# Orchestrates deployment order: namespace -> postgres -> notes
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: sovereign-product
spec:
  schema:
    apiVersion: v1alpha1
    kind: SovereignProduct
    spec:
      namespace: string | default="sovereign-product"
      # Production overrides
      notes:
        replicas: integer | default=3
      postgres:
        storageSize: string | default="10Gi"
        storageClass: string | default="fast-ssd"
  resources:
    # Deploy namespace first
    - id: namespace
      template:
        apiVersion: v1
        kind: Namespace
        metadata:
          name: ${schema.spec.namespace}
    # PostgreSQL instance (depends on namespace)
    - id: postgres
      template:
        apiVersion: kro.run/v1alpha1
        kind: SovereignPostgres
        metadata:
          name: postgres
          namespace: ${namespace.metadata.name}
        spec:
          namespace: ${schema.spec.namespace}
          storage:
            size: ${schema.spec.postgres.storageSize}
            storageClass: ${schema.spec.postgres.storageClass}
    # Notes instance (depends on postgres)
    - id: notes
      template:
        apiVersion: kro.run/v1alpha1
        kind: SovereignNotes
        metadata:
          name: notes
          namespace: ${namespace.metadata.name}
        spec:
          namespace: ${schema.spec.namespace}
          replicas: ${schema.spec.notes.replicas}
```

</details>

### 4.4 Settings File

<details>
<summary><code>settings.yaml</code></summary>

```yaml
# settings.yaml
VERSION: 1.0.0
POSTGRES_VERSION: "16-alpine"
```

</details>

Note: Environment-specific configuration (replicas, resources, storage) is now defined via RGD instance specs rather than component resources. Database credentials should be managed via Kubernetes secrets (e.g., ExternalSecrets, sealed-secrets) rather than embedded in components.

---

## 5. Signing Workflow

<details open>
<summary>Diagram: Signing Workflow</summary>

```mermaid
flowchart LR
    subgraph build[Build]
        ctf[CTF] --> sign[ocm sign]
        sign --> signed[Signed CTF]
    end

    subgraph transfer[Transfer]
        signed --> push[ocm transfer ctf]
        push --> ghcr[(ghcr.io)]
        ghcr --> pull[ocm transfer cv --verify]
        pull --> verified[Verified CTF]
    end

    subgraph deploy[Deploy]
        verified --> load[ocm transfer ctf]
        load --> local[(localhost:5001)]
        local --> controller[OCM Controller]
        controller --> secret[K8s Secret]
        secret -->|verified| reconcile[Reconcile]
    end

    privkey[Private Key] -.-> sign
    pubkey[Public Key] -.-> pull
    pubkey -.-> secret
```

</details>

### 5.1 Key Generation (One-time Setup)

<details>
<summary>Key generation commands</summary>

```bash
# Generate RSA key pair for signing
openssl genpkey -algorithm RSA -out keys/acme-private.pem -pkeyopt rsa_keygen_bits:4096
openssl rsa -pubout -in keys/acme-private.pem -out keys/acme-public.pem

# Store private key securely (CI secret, HSM, etc.)
# Public key is bundled in the product component
```

</details>

<details>
<summary>Credential config for signers</summary>

```yaml
## Example Credential Config for signers
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        private_key_pem: <PEM>
        # private_key_pem_file: <path to PEM file>
```

</details>

<details>
<summary>Credential config for verifiers</summary>

```yaml
## Example Credential Config for verifiers
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem: <PEM>
        # public_key_pem_file: <path to PEM file>
```

</details>

### 5.2 Sign During Build

<details>
<summary>Sign command</summary>

```bash
# After building CTF, sign all component versions
ocm sign componentversion
```

</details>

### 5.3 Verify Before Transfer

_NOTE: We can currently only verify signatures for cvs already transferred or in archives. We need a transformer here to bridge this later._

<details>
<summary>Verify and transfer commands</summary>

```bash
# When transferring to air-gap, verify signatures
ocm verify cv ghcr.io/ocm/reference-scenario//acme.org/sovereign/product:${VERSION}
ocm transfer cv \
  --recursive \
  --copy-resources \
  ghcr.io/ocm/reference-scenario//acme.org/sovereign/product:${VERSION} \
  ./transport-archive
```

</details>

### 5.4 Verify on Cluster

<details>
<summary><code>deploy/component.yaml</code> — Component with verification</summary>

```yaml
# deploy/component.yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: sovereign-product
  namespace: ocm-system
spec:
  component: acme.org/sovereign/product
  repositoryRef:
    name: sovereign-repo
  semver: ">=1.0.0"
  interval: 10m
  verify:
    - signature: acme-signature
      secretRef:
        name: acme-signing-key
---
# The public key secret (created from the signing-public-key resource or pre-provisioned)
apiVersion: v1
kind: Secret
metadata:
  name: acme-signing-key
  namespace: ocm-system
type: Opaque
data:
  key: |  # base64-encoded public key
    LS0tLS1CRUdJTi...
```

</details>

---

## 6. Configuration via ResourceGraphDefinition

> **Where this fits:** [kro](https://kro.run/) provides the configuration layer between OCM components and Kubernetes workloads. RGDs define _how_ components are deployed, while RGD instances provide _environment-specific values_. This separation enables the same component to be deployed differently across sovereign environments.

**Why kro matters for sovereign delivery:**

- **Strongly-typed configuration**: JSON Schema validates values before deployment
- **CEL expressions**: Transform image references for local registries without changing components
- **Environment isolation**: Each sovereign environment gets its own RGD instance with specific settings
- **Declarative templates**: Infrastructure-as-code approach aligns with GitOps workflows

<details open>
<summary>Diagram: Configuration Flow</summary>

```mermaid
flowchart TB
    subgraph component[OCM Component]
        chart[helm-chart]
        image[oci-image]
        rgd[rgd-blob]
    end

    subgraph controller[OCM Controller]
        res_chart[Resource: chart]
        res_image[Resource: image]
        deployer[Deployer]
    end

    subgraph kro[kro]
        rgd_inst[RGD Instance]
        schema[Schema Values]
    end

    subgraph flux[Flux]
        oci_repo[OCIRepository]
        helm_rel[HelmRelease]
    end

    chart --> res_chart
    image --> res_image
    rgd --> deployer
    res_chart --> deployer
    res_image --> deployer
    deployer --> rgd_inst
    schema -->|values| rgd_inst
    rgd_inst --> oci_repo
    rgd_inst --> helm_rel
    helm_rel -->|apply| k8s[Kubernetes]
```

</details>

The key insight is that **configuration flows through ResourceGraphDefinition (RGD) schemas**, not separate config resources. This enables:

- Strongly-typed configuration via RGD schema definitions
- Image localization via `additionalStatusFields` CEL expressions
- Environment-specific values via RGD instance specs
- Air-gap friendly (images referenced through Resource status)

### 6.1 Resource with Image Extraction

<details>
<summary><code>deploy/resource-notes-image.yaml</code></summary>

```yaml
# deploy/resource-notes-image.yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: notes-image
  namespace: ocm-system
spec:
  interval: 10m
  componentRef:
    name: sovereign-notes
    namespace: ocm-system
  resource:
    byReference:
      resource:
        name: image
  # Extract image components for use in RGD templates
  additionalStatusFields:
    registry: resource.access.imageReference.toOCI().registry
    repository: resource.access.imageReference.toOCI().repository
    tag: resource.access.imageReference.toOCI().tag
```

</details>

### 6.2 ResourceGraphDefinition for Deployment

The RGD defines a custom schema and templates for FluxCD resources:

<details>
<summary><code>components/notes/deploy/rgd.yaml</code> — Full RGD</summary>

```yaml
# components/notes/deploy/rgd.yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: sovereign-notes
spec:
  schema:
    apiVersion: v1alpha1
    kind: SovereignNotes
    spec:
      # Configuration values with defaults
      releaseName: string | default="sovereign-notes"
      namespace: string | default="sovereign-product"
      replicas: integer | default=2
      resources:
        requests:
          memory: string | default="64Mi"
          cpu: string | default="100m"
        limits:
          memory: string | default="128Mi"
          cpu: string | default="200m"

  resources:
    # FluxCD OCIRepository for Helm chart
    - id: ociRepository
      template:
        apiVersion: source.toolkit.fluxcd.io/v1beta2
        kind: OCIRepository
        metadata:
          name: ${schema.spec.releaseName}
          namespace: ${schema.spec.namespace}
        spec:
          interval: 10m
          url: oci://${resourceChart.status.additional.registry}/${resourceChart.status.additional.repository}
          ref:
            tag: ${resourceChart.status.additional.tag}

    # FluxCD HelmRelease
    - id: helmRelease
      template:
        apiVersion: helm.toolkit.fluxcd.io/v2
        kind: HelmRelease
        metadata:
          name: ${schema.spec.releaseName}
          namespace: ${schema.spec.namespace}
        spec:
          interval: 10m
          chart:
            spec:
              chart: .
              sourceRef:
                kind: OCIRepository
                name: ${ociRepository.metadata.name}
          values:
            replicaCount: ${schema.spec.replicas}
            image:
              repository: ${resourceImage.status.additional.registry}/${resourceImage.status.additional.repository}
              tag: ${resourceImage.status.additional.tag}
            resources:
              requests:
                memory: ${schema.spec.resources.requests.memory}
                cpu: ${schema.spec.resources.requests.cpu}
              limits:
                memory: ${schema.spec.resources.limits.memory}
                cpu: ${schema.spec.resources.limits.cpu}
```

</details>

### 6.3 RGD Instance for Environment Configuration

<details>
<summary><code>deploy/notes-instance.yaml</code></summary>

```yaml
# deploy/notes-instance.yaml
apiVersion: kro.run/v1alpha1
kind: SovereignNotes
metadata:
  name: notes-production
  namespace: ocm-system
spec:
  releaseName: sovereign-notes
  namespace: sovereign-product
  replicas: 3
  resources:
    requests:
      memory: "128Mi"
      cpu: "200m"
    limits:
      memory: "256Mi"
      cpu: "500m"
```

</details>

---

## 7. Build & Publish Pipeline

### 7.1 Taskfile (Local Development)

<details>
<summary><code>Taskfile.yml</code></summary>

```yaml
# Taskfile.yml
version: '3'

vars:
  VERSION: '{{.VERSION | default "1.0.0"}}'
  CTF: transport-archive
  OCM_REPO: ghcr.io/open-component-model/reference-scenario

tasks:
  # Build the notes application
  build:app:
    dir: components/notes
    cmds:
      - docker buildx build
          --platform linux/amd64,linux/arm64
          -t acme.org/sovereign/notes:{{.VERSION}}
          --load .

  # Build all components into CTF
  build:ctf:
    cmds:
      - rm -rf {{.CTF}}
      - ocm add cv --create --file {{.CTF}}
          --settings settings.yaml
          components/notes/component-constructor.yaml
      - ocm add cv --file {{.CTF}}
          --settings settings.yaml
          components/postgres/component-constructor.yaml
      - ocm add cv --file {{.CTF}}
          --settings settings.yaml
          components/product/component-constructor.yaml

  # Sign all components
  sign:
    deps: [build:ctf]
    cmds:
      - ocm sign componentversion
          --signature acme-signature
          --private-key keys/acme-private.pem
          --recursive
          {{.CTF}}//acme.org/sovereign/product:{{.VERSION}}

  # Verify signatures
  verify:
    cmds:
      - ocm verify componentversion
          --signature acme-signature
          --public-key keys/acme-public.pem
          {{.CTF}}//acme.org/sovereign/product:{{.VERSION}}

  # Push to registry
  push:
    deps: [sign]
    cmds:
      - ocm transfer ctf
          --copy-resources
          --enforce
          --overwrite
          {{.CTF}} {{.OCM_REPO}}

  # Full build + sign + push
  release:
    deps: [push]

  # Transfer to air-gap archive (with verification)
  transfer:airgap:
    cmds:
      - ocm transfer cv
          --recursive
          --copy-resources
          --verify acme-signature=keys/acme-public.pem
          {{.OCM_REPO}}//acme.org/sovereign/product:{{.VERSION}}
          ./airgap-archive

  # Set up local kind cluster
  cluster:create:
    cmds:
      - bash scripts/setup-airgapped-kind.sh

  # Transfer archive to local registry
  cluster:load:
    cmds:
      - ocm transfer ctf
          --copy-resources
          --enforce
          --overwrite
          ./airgap-archive
          localhost:5001

  # Deploy to cluster
  cluster:deploy:
    cmds:
      - kubectl apply -f deploy/

  # Full local demo
  demo:
    cmds:
      - task: build:ctf
      - task: sign
      - task: transfer:airgap
      - task: cluster:create
      - task: cluster:load
      - task: cluster:deploy
      - task: verify:deployment

  verify:deployment:
    cmds:
      - kubectl -n ocm-system wait --for=condition=Ready
          component/sovereign-product --timeout=5m
      - kubectl -n sovereign-product wait --for=condition=Available
          deployment/notes --timeout=3m
      - kubectl -n sovereign-product wait --for=condition=Ready
          pod -l app=postgres --timeout=3m
      - echo "✅ Deployment verified"
```

</details>

### 7.2 GitHub Actions Pipeline

<details>
<summary><code>.github/workflows/release-reference-scenario.yaml</code></summary>

```yaml
# .github/workflows/release-reference-scenario.yaml
name: Release Reference Scenario

on:
  push:
    tags: ["v*"]
  workflow_dispatch:
    inputs:
      version:
        description: "Component version (without v prefix)"
        required: true
        default: "1.0.0"

permissions:
  packages: write
  contents: read

env:
  OCM_REPO: ghcr.io/${{ github.repository_owner }}/reference-scenario
  VERSION: ${{ github.event.inputs.version || github.ref_name }}

jobs:
  build-sign-push:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Setup OCM CLI
        uses: open-component-model/ocm-setup-action@main

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Login to GHCR
        run: |
          echo "${{ secrets.GITHUB_TOKEN }}" | \
            ocm login -u ${{ github.actor }} --password-stdin ghcr.io

      - name: Build notes application image
        working-directory: reference-scenario/components/notes
        run: |
          docker buildx build \
            --platform linux/amd64,linux/arm64 \
            -t acme.org/sovereign/notes:${{ env.VERSION }} \
            --load .

      - name: Build CTF
        working-directory: reference-scenario
        run: |
          VERSION=${{ env.VERSION }} task build:ctf

      - name: Sign components
        working-directory: reference-scenario
        env:
          SIGNING_KEY: ${{ secrets.OCM_SIGNING_PRIVATE_KEY }}
        run: |
          echo "$SIGNING_KEY" > /tmp/private.pem
          ocm sign componentversion \
            --signature acme-signature \
            --private-key /tmp/private.pem \
            --recursive \
            transport-archive//acme.org/sovereign/product:${{ env.VERSION }}
          rm /tmp/private.pem

      - name: Verify signatures
        working-directory: reference-scenario
        run: |
          ocm verify componentversion \
            --signature acme-signature \
            --public-key keys/acme-public.pem \
            transport-archive//acme.org/sovereign/product:${{ env.VERSION }}

      - name: Push to GHCR
        working-directory: reference-scenario
        run: |
          ocm transfer ctf \
            --copy-resources \
            --enforce \
            --overwrite \
            transport-archive ${{ env.OCM_REPO }}

      - name: Upload CTF artifact
        uses: actions/upload-artifact@v4
        with:
          name: transport-archive-${{ env.VERSION }}
          path: reference-scenario/transport-archive

  integration-test:
    needs: build-sign-push
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Setup OCM CLI
        uses: open-component-model/ocm-setup-action@main

      - name: Create kind cluster
        uses: helm/kind-action@v1
        with:
          cluster_name: airgapped-test

      - name: Download CTF
        uses: actions/download-artifact@v4
        with:
          name: transport-archive-${{ env.VERSION }}
          path: reference-scenario/transport-archive

      - name: Set up local registry
        run: |
          docker run -d -p 5001:5000 --name registry registry:2
          docker network connect kind registry

      - name: Transfer to local registry
        working-directory: reference-scenario
        run: |
          ocm transfer ctf \
            --copy-resources \
            --enforce \
            transport-archive localhost:5001

      - name: Install OCM controller
        run: |
          kubectl apply -f https://github.com/open-component-model/open-component-model/releases/latest/download/install.yaml
          kubectl -n ocm-system wait --for=condition=Available deployment/ocm-controller --timeout=120s

      - name: Create signing key secret
        working-directory: reference-scenario
        run: |
          kubectl -n ocm-system create secret generic acme-signing-key \
            --from-file=key=keys/acme-public.pem

      - name: Deploy components
        working-directory: reference-scenario
        run: |
          kubectl apply -f deploy/

      - name: Verify deployment
        run: |
          kubectl -n ocm-system wait --for=condition=Ready \
            component/sovereign-product --timeout=5m
          kubectl -n sovereign-product wait --for=condition=Available \
            deployment/notes --timeout=3m
          kubectl -n sovereign-product wait --for=condition=Ready \
            pod -l app=postgres --timeout=3m

      - name: Test connectivity
        run: |
          kubectl -n sovereign-product port-forward svc/notes 8080:80 &
          sleep 5
          curl -f http://localhost:8080/readyz
          curl -f http://localhost:8080/notes
```

</details>

---

## 8. OCM Controller Deployment Manifests

### 8.1 Repository

<details>
<summary><code>deploy/repository.yaml</code></summary>

```yaml
# deploy/repository.yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: sovereign-repo
  namespace: ocm-system
spec:
  repositorySpec:
    baseUrl: localhost:5001
    type: OCIRegistry
  interval: 10m
  ocmConfig:
    - secretRef:
        name: registry-credentials
```

</details>

### 8.2 Components

<details>
<summary><code>deploy/components.yaml</code></summary>

```yaml
# deploy/components.yaml
---
# Product component (aggregates notes and postgres)
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: sovereign-product
  namespace: ocm-system
spec:
  component: acme.org/sovereign/product
  repositoryRef:
    name: sovereign-repo
  semver: ">=1.0.0"
  interval: 10m
  verify:
    - signature: acme-signature
      secretRef:
        name: acme-signing-key
---
# Notes component (referenced by product)
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: sovereign-notes
  namespace: ocm-system
spec:
  component: acme.org/sovereign/notes
  repositoryRef:
    name: sovereign-repo
  semver: ">=1.0.0"
  interval: 10m
---
# PostgreSQL component (referenced by product)
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: sovereign-postgres
  namespace: ocm-system
spec:
  component: acme.org/sovereign/postgres
  repositoryRef:
    name: sovereign-repo
  semver: ">=1.0.0"
  interval: 10m
```

</details>

### 8.3 Resources

<details>
<summary><code>deploy/resources.yaml</code></summary>

```yaml
# deploy/resources.yaml
---
# Notes Helm chart
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: notes-chart
  namespace: ocm-system
spec:
  interval: 10m
  componentRef:
    name: sovereign-notes
    namespace: ocm-system
  resource:
    byReference:
      resource:
        name: helm-chart
  additionalStatusFields:
    registry: resource.access.imageReference.toOCI().registry
    repository: resource.access.imageReference.toOCI().repository
    tag: resource.access.imageReference.toOCI().tag
---
# Notes container image
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: notes-image
  namespace: ocm-system
spec:
  interval: 10m
  componentRef:
    name: sovereign-notes
    namespace: ocm-system
  resource:
    byReference:
      resource:
        name: image
  additionalStatusFields:
    registry: resource.access.imageReference.toOCI().registry
    repository: resource.access.imageReference.toOCI().repository
    tag: resource.access.imageReference.toOCI().tag
---
# PostgreSQL Helm chart
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: postgres-chart
  namespace: ocm-system
spec:
  interval: 10m
  componentRef:
    name: sovereign-postgres
    namespace: ocm-system
  resource:
    byReference:
      resource:
        name: helm-chart
  additionalStatusFields:
    registry: resource.access.imageReference.toOCI().registry
    repository: resource.access.imageReference.toOCI().repository
    tag: resource.access.imageReference.toOCI().tag
---
# PostgreSQL container image
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: postgres-image
  namespace: ocm-system
spec:
  interval: 10m
  componentRef:
    name: sovereign-postgres
    namespace: ocm-system
  resource:
    byReference:
      resource:
        name: image
  additionalStatusFields:
    registry: resource.access.imageReference.toOCI().registry
    repository: resource.access.imageReference.toOCI().repository
    tag: resource.access.imageReference.toOCI().tag
---
# Notes RGD (ResourceGraphDefinition)
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: notes-rgd
  namespace: ocm-system
spec:
  interval: 10m
  componentRef:
    name: sovereign-notes
    namespace: ocm-system
  resource:
    byReference:
      resource:
        name: rgd
---
# PostgreSQL RGD
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: postgres-rgd
  namespace: ocm-system
spec:
  interval: 10m
  componentRef:
    name: sovereign-postgres
    namespace: ocm-system
  resource:
    byReference:
      resource:
        name: rgd
```

</details>

### 8.4 Deployers

<details>
<summary><code>deploy/deployers.yaml</code></summary>

```yaml
# deploy/deployers.yaml
---
# PostgreSQL deployer (deploys first)
apiVersion: delivery.ocm.software/v1alpha1
kind: Deployer
metadata:
  name: postgres
spec:
  resourceRef:
    name: postgres-rgd
    namespace: ocm-system
---
# Notes deployer (depends on postgres via RGD)
apiVersion: delivery.ocm.software/v1alpha1
kind: Deployer
metadata:
  name: notes
spec:
  resourceRef:
    name: notes-rgd
    namespace: ocm-system
```

</details>

### 8.5 RGD Instances (Configuration)

<details>
<summary><code>deploy/instances.yaml</code></summary>

```yaml
# deploy/instances.yaml
---
# PostgreSQL instance with production values
apiVersion: kro.run/v1alpha1
kind: SovereignPostgres
metadata:
  name: postgres-production
  namespace: ocm-system
spec:
  releaseName: sovereign-postgres
  namespace: sovereign-product
  storage:
    size: 10Gi
    storageClass: "fast-ssd"
  resources:
    requests:
      memory: "512Mi"
      cpu: "500m"
    limits:
      memory: "1Gi"
      cpu: "1000m"
---
# Notes instance with production values
apiVersion: kro.run/v1alpha1
kind: SovereignNotes
metadata:
  name: notes-production
  namespace: ocm-system
spec:
  releaseName: sovereign-notes
  namespace: sovereign-product
  replicas: 3
  resources:
    requests:
      memory: "128Mi"
      cpu: "200m"
    limits:
      memory: "256Mi"
      cpu: "500m"
  # Database connection injected via secret reference
  databaseSecretRef: db-credentials
```

</details>

---

## 9. Upgrade Scenario

### 9.0 Upgrade Flow Overview

<details open>
<summary>Diagram: Upgrade Sequence</summary>

```mermaid
sequenceDiagram
    participant Dev as Developer
    participant CI as CI Pipeline
    participant GHCR as ghcr.io
    participant CTF as CTF Archive
    participant Airgap as Air-Gap Registry
    participant Controller as OCM Controller
    participant Flux as Flux
    participant K8s as Kubernetes

    Note over Dev,K8s: v1.0.0 already deployed

    Dev->>CI: Push v1.1.0 tag
    CI->>CI: Build + Sign
    CI->>GHCR: Transfer CTF

    Note over GHCR,CTF: Air-gap transfer
    GHCR->>CTF: Transfer with verify
    CTF-->>Airgap: Physical transfer

    Note over Controller,K8s: Auto reconciliation
    Controller->>Airgap: Detect v1.1.0
    Controller->>Controller: Verify signature
    Controller->>Flux: Trigger reconcile
    Flux->>K8s: Rolling update
```

</details>

<details open>
<summary>Diagram: Upgrade Flow</summary>

```mermaid
flowchart LR
    v1[v1.0.0 deployed]
    v1 --> build[Build v1.1.0]
    build --> sign[Sign]
    sign --> transfer[Transfer]
    transfer --> detect[Detect]
    detect --> verify[Verify]
    verify --> reconcile[Reconcile]
    reconcile --> v2[v1.1.0 deployed]
```

</details>

### 9.1 Version Bump

<details>
<summary><code>settings.yaml</code> (v1.1.0)</summary>

```yaml
# settings.yaml (v1.1.0)
VERSION: 1.1.0
POSTGRES_VERSION: "16-alpine"  # unchanged
DB_PASSWORD: "changeme-in-production"
```

</details>

With code changes:

<details>
<summary>New endpoint in sovereign-notes</summary>

```go
// Add a new endpoint in sovereign-notes
http.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(map[string]string{"version": "1.1.0"})
})
```

</details>

### 9.2 Release New Version

<details>
<summary>Release commands</summary>

```bash
# Build, sign, push v1.1.0
VERSION=1.1.0 task release

# Transfer to air-gap
VERSION=1.1.0 task transfer:airgap

# Load into local registry
task cluster:load
```

</details>

### 9.3 Automatic Controller Reconciliation

The `Component` CR has `semver: ">=1.0.0"`, so when `1.1.0` appears in the registry:

1. Controller detects new version matching constraint
2. Verifies signature with `acme-signing-key`
3. Updates `Component` status to `1.1.0`
4. Triggers reconciliation of all dependent `Resource` CRs
5. `Deployer` CRs detect new resource versions
6. RGD instances trigger FluxCD `HelmRelease` updates
7. Kubernetes performs rolling update of Deployments

**Verification:**

<details>
<summary>Verification commands</summary>

```bash
# Watch version transition
kubectl -n ocm-system get component sovereign-product -w

# Verify new version is running
kubectl -n sovereign-product get deployment notes -o jsonpath='{.spec.template.spec.containers[0].image}'

# Test new endpoint
kubectl -n sovereign-product port-forward svc/notes 8080:80 &
curl http://localhost:8080/version
# {"version":"1.1.0"}
```

</details>

---

## 10. Repository Layout

<details>
<summary>Repository structure</summary>

```
reference-scenario/
├── README.md
├── settings.yaml
├── Taskfile.yml
│
├── keys/
│   ├── acme-private.pem          # NOT committed (CI secret)
│   └── acme-public.pem           # Committed, bundled in product
│
├── components/
│   ├── notes/
│   │   ├── component-constructor.yaml
│   │   ├── Dockerfile
│   │   ├── go.mod
│   │   ├── go.sum
│   │   ├── main.go
│   │   ├── ord.go                # ORD endpoint handlers
│   │   └── deploy/
│   │       ├── chart/            # Helm chart
│   │       │   ├── Chart.yaml
│   │       │   ├── values.yaml
│   │       │   └── templates/
│   │       │       ├── deployment.yaml
│   │       │       └── service.yaml
│   │       ├── rgd.yaml          # ResourceGraphDefinition
│   │       ├── ord/              # Open Resource Discovery
│   │       │   └── document.json # ORD metadata document
│   │       └── openapi/
│   │           └── spec.yaml     # OpenAPI specification
│   │
│   ├── postgres/
│   │   ├── component-constructor.yaml
│   │   └── deploy/
│   │       ├── chart/            # Helm chart
│   │       │   ├── Chart.yaml
│   │       │   ├── values.yaml
│   │       │   └── templates/
│   │       │       ├── statefulset.yaml
│   │       │       ├── service.yaml
│   │       │       └── pvc.yaml
│   │       └── rgd.yaml          # ResourceGraphDefinition
│   │
│   └── product/
│       ├── component-constructor.yaml
│       └── deploy/
│           ├── base.yaml         # Namespace, secrets
│           └── rgd.yaml          # Product orchestration RGD
│
├── deploy/                        # OCM controller CRDs
│   ├── repository.yaml
│   ├── components.yaml
│   ├── resources.yaml
│   ├── deployers.yaml
│   └── instances.yaml            # RGD instances (env config)
│
├── scripts/
│   ├── setup-airgapped-kind.sh
│   └── generate-keys.sh
│
└── tests/
    ├── integration/
    │   ├── scenario_test.go
    │   └── upgrade_test.go
    └── e2e/
        └── smoke_test.sh
```

</details>

---

## 11. Integration Points for Upstream Testing

| Integration Point                              | Test Type   | Validates                                                  |
|------------------------------------------------|-------------|------------------------------------------------------------|
| `ocm add cv` with `component-constructor.yaml` | Unit        | Component construction, resource bundling                  |
| `ocm sign` / `ocm verify`                      | Unit        | Signing workflow, key handling                             |
| `ocm transfer ctf --copy-resources`            | Unit        | Resource localization, self-contained archive              |
| `ocm transfer cv --verify`                     | Integration | Signature verification during transfer                     |
| `Repository` CR reconciliation                 | Integration | Controller validates registry connection                   |
| `Component` CR reconciliation                  | Integration | Controller fetches from registry, verifies signature       |
| `Resource` CR with `referencePath`             | Integration | Component reference traversal                              |
| `Resource` CR with `additionalStatusFields`    | Integration | CEL expression evaluation for image extraction             |
| `Deployer` CR with RGD                         | Integration | RGD instantiation and FluxCD resource creation             |
| Upgrade detection (semver constraint)          | Integration | Version bump triggers reconciliation                       |
| ORD configuration endpoint                     | Integration | `.well-known/open-resource-discovery` returns valid config |
| ORD document endpoint                          | Integration | ORD document describes APIs, events, dependencies          |
| End-to-end air-gap flow                        | E2E         | Full scenario from build to running workload               |

### Test Commands

<details>
<summary>Test commands</summary>

```bash
# Run unit tests (fast, no cluster needed)
task test

# Run integration tests (requires Docker + kind)
task test/integration

# Run full E2E (builds, signs, transfers, deploys, verifies)
task demo
```

</details>

---

## 11. Local Development Workflow

This section provides guidance for developers working with the sovereign cloud reference scenario locally.

### 11.1 Prerequisites

- **Docker**: For building container images
- **kind**: For local Kubernetes clusters
- **kubectl**: For cluster interaction
- **OCM CLI**: For component operations
- **Task**: For running build automation (optional)

### 11.2 Quick Setup

```bash
# Clone the reference scenario
git clone https://github.com/open-component-model/reference-scenario
cd reference-scenario

# Create local development environment
make setup-local

# Build and deploy locally
task build-and-deploy

# Verify deployment
curl http://localhost:8080/notes
```

### 11.3 Development Cycle

1. **Make changes** to application code or components
2. **Build components** with `task build-ctf`
3. **Deploy locally** with `task deploy-local`
4. **Test functionality** with `task test`
5. **Iterate** as needed

### 11.4 Component Development

```bash
# Build individual components
ocm create component-constructor ./components/notes/component-constructor.yaml

# Test component locally
ocm get cv ./gen/ctf//acme.org/sovereign/notes:${VERSION}

# Sign component (for testing)
ocm sign cv ./gen/ctf//acme.org/sovereign/notes:${VERSION} --signing-key=./dev-keys/private.pem
```

### 11.5 Air-Gap Testing

```bash
# Simulate air-gap transfer
task export-ctf
# [Transfer CTF via secure media]
task import-ctf

# Verify air-gap deployment
kubectl port-forward svc/sovereign-notes 8080:8080
curl http://localhost:8080/healthz
```

---

## 12. Testing and Troubleshooting

### 12.1 Testing Strategy

The reference scenario includes multiple levels of testing:

| Test Type             | Scope                       | Tools             | Duration      |
|-----------------------|-----------------------------|-------------------|---------------|
| **Unit Tests**        | Individual components       | Go test, Jest     | 1-2 minutes   |
| **Integration Tests** | OCM controller interactions | kind, kubectl     | 5-10 minutes  |
| **End-to-End Tests**  | Complete deployment flow    | Bash scripts      | 15-30 minutes |
| **Air-Gap Tests**     | CTF transfer validation     | Manual simulation | 10-15 minutes |

### 12.2 Test Commands

```bash
# Run all tests
task test

# Unit tests only
task test-unit

# Integration tests with kind
task test-integration

# End-to-end tests
task test-e2e

# Air-gap simulation
task test-airgap
```

## 13. Deployment Extensibility

> **Where this fits:** This section explains how OCM + kro integrates with GitOps tools. FluxCD is the primary deployment target in this reference scenario, but the architecture supports other tools.

**Why FluxCD for sovereign delivery:**

- **Mature and production-ready**: CNCF graduated project with wide adoption
- **Native OCI support**: Pulls artifacts directly from OCI registries (no Git required in air-gap)
- **Drift detection**: Automatically reconciles if workloads diverge from desired state
- **Multi-tenancy**: Supports multiple environments with different configurations
- **Helm + Kustomize**: Deploys both Helm charts and raw manifests

<details open>
<summary>Diagram: Deployment Extensibility</summary>

```mermaid
flowchart TB
    subgraph ocm[OCM Layer]
        repo[Repository]
        comp[Component]
        res[Resource]
        repo --> comp --> res
    end

    subgraph deployers[Deployers]
        deployer[Deployer]
        rgd[RGD Templates]
    end

    subgraph targets[Deployment Targets]
        flux[FluxCD]
        argo[ArgoCD]
        helm[Helm]
        raw[Raw Manifests]
    end

    res --> deployer
    deployer --> rgd
    rgd --> flux
    rgd -.-> argo
    rgd -.-> helm
    rgd -.-> raw

    flux --> k8s[Kubernetes]
    argo -.-> k8s
    helm -.-> k8s
    raw -.-> k8s
```

</details>

This design uses **kro ResourceGraphDefinitions** for deployment orchestration, enabling flexible target systems:

| Target               | How it integrates                                                       | Air-Gap Support                       |
|----------------------|-------------------------------------------------------------------------|---------------------------------------|
| **FluxCD** (default) | RGD templates create `OCIRepository` + `HelmRelease` or `Kustomization` | Excellent — native OCI, no Git needed |
| **ArgoCD**           | RGD templates create `Application` CRs                                  | Good — supports OCI Helm charts       |
| **Raw Manifests**    | RGD templates output Kubernetes YAML directly                           | Manual — kubectl apply                |

The **component structure remains unchanged** — only the RGD templates vary per deployment target. This means:

- Same OCM component works with FluxCD in production and ArgoCD in staging
- Air-gapped environments can use whichever GitOps tool is approved
- Migration between tools doesn't require component changes

---

## 14. Key Design Decisions

| Decision                               | Rationale                                                                                                                |
|----------------------------------------|--------------------------------------------------------------------------------------------------------------------------|
| **Custom Go app (sovereign-notes)**    | Real PostgreSQL dependency, full build pipeline demo, tiny and understandable                                            |
| **RGD-based configuration**            | Strongly-typed values via schema, CEL expressions for image localization                                                 |
| **RSA signing with verification**      | Meets sovereign cloud security requirements with own PKI, can be replaced with keyless                                   |
| **kro + FluxCD deployment**            | RGDs provide flexibility; FluxCD is mature and widely deployed                                                           |
| **kind with local registry**           | Fully reproducible locally, simulates air-gap registry                                                                   |
| **semver constraint for upgrades**     | Controller auto-detects new versions without CR changes                                                                  |
| **additionalStatusFields for images**  | CEL expressions extract registry/repo/tag for localization without separate CR                                           |
| **ORD for service discovery**          | Decentralized metadata discovery; services self-describe via standard protocol                                           |
| **Global/Local Control Planes**        | Global MCP manages components in connected env; Local MCPs deploy in air-gapped isolation                                |
| **Platform Mesh for service ordering** | KRM-based service catalog with automatic dependency resolution; integrates with OpenMCP for multi-control-plane delivery |
| **External Secrets for credentials**   | Secure secret injection from external stores (Vault); decouples credential management from application deployment        |
