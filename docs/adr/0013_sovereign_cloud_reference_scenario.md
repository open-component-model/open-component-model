# Design: Reference Scenario — Sovereign Cloud Delivery

* **Status**: proposed
* **Deciders**: Gergely Brautigam, Fabian Burth, Jakob Moeller
* **Date**: 2025-02-06

**Issue:** [ocm-project#842](https://github.com/open-component-model/ocm-project/issues/842)  
**Parent Epic:** [ocm-project#424 — OCM Reference Scenario / Golden Path](https://github.com/open-component-model/ocm-project/issues/424)  
**Related:** [ocm-project#843 — Document and Describe Reference Scenario in "How To's"](https://github.com/open-component-model/ocm-project/issues/843)

## Table of Contents

<!-- TOC -->
* [Design: Reference Scenario — Sovereign Cloud Delivery](#design-reference-scenario--sovereign-cloud-delivery)
  * [Table of Contents](#table-of-contents)
  * [1. Overview](#1-overview)
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
  * [12. Open Resource Discovery (ORD) Integration](#12-open-resource-discovery-ord-integration)
    * [12.1 Architecture Overview](#121-architecture-overview)
    * [12.2 ORD Configuration Endpoint](#122-ord-configuration-endpoint)
    * [12.3 ORD Document: Describing sovereign-notes](#123-ord-document-describing-sovereign-notes)
    * [12.4 OCM Component with ORD Resource](#124-ocm-component-with-ord-resource)
    * [12.5 ORD Aggregator Integration](#125-ord-aggregator-integration)
    * [12.6 Deployment with ORD Metadata](#126-deployment-with-ord-metadata)
    * [12.7 sovereign-notes ORD Implementation](#127-sovereign-notes-ord-implementation)
    * [12.8 OpenAPI Specification](#128-openapi-specification)
    * [12.9 Integration with Service Catalogs](#129-integration-with-service-catalogs)
  * [13. OpenMCP Integration (Multi-Control-Plane Deployment)](#13-openmcp-integration-multi-control-plane-deployment)
    * [13.1 Sovereign Delivery Model: Global vs Local Control Planes](#131-sovereign-delivery-model-global-vs-local-control-planes)
    * [13.2 Architecture Overview](#132-architecture-overview)
    * [13.3 OpenMCP Resource Hierarchy](#133-openmcp-resource-hierarchy)
    * [13.4 Global Control Plane Configuration](#134-global-control-plane-configuration)
    * [13.5 Local Control Plane Configuration (Sovereign/Air-Gapped)](#135-local-control-plane-configuration-sovereignair-gapped)
    * [13.6 Global Control Plane: Component Management](#136-global-control-plane-component-management)
    * [13.7 Air-Gap Transfer: Global to Local](#137-air-gap-transfer-global-to-local)
    * [13.8 Local Control Plane: Deployment](#138-local-control-plane-deployment)
    * [13.9 Environment-Specific RGD Instances](#139-environment-specific-rgd-instances)
    * [13.10 Global-to-Local Deployment Flow](#1310-global-to-local-deployment-flow)
    * [13.11 Cross-MCP Service Discovery (via ORD)](#1311-cross-mcp-service-discovery-via-ord)
    * [13.12 Benefits of Global/Local Control Plane Architecture](#1312-benefits-of-globallocal-control-plane-architecture)
    * [13.13 Summary: MCP Mapping to Sovereign Delivery](#1313-summary-mcp-mapping-to-sovereign-delivery)
  * [14. Platform Mesh Integration (Service Ordering API)](#14-platform-mesh-integration-service-ordering-api)
    * [14.1 Architecture Overview](#141-architecture-overview)
    * [14.2 Service Provider: Exporting sovereign-notes](#142-service-provider-exporting-sovereign-notes)
    * [14.3 Service Consumer: Ordering sovereign-notes](#143-service-consumer-ordering-sovereign-notes)
    * [14.4 Dependency Ordering](#144-dependency-ordering)
    * [14.5 Integration with OCM Controller](#145-integration-with-ocm-controller)
    * [14.6 Multi-Provider Scenarios](#146-multi-provider-scenarios)
    * [14.7 End-to-End Flow: Platform Mesh → OpenMCP → OCM](#147-end-to-end-flow-platform-mesh--openmcp--ocm)
  * [15. Deployment Extensibility](#15-deployment-extensibility)
  * [16. Key Design Decisions](#16-key-design-decisions)
<!-- TOC -->

---

## 1. Overview

This document designs a reference scenario demonstrating OCM's core value proposition: **modeling, signing, transporting, and deploying a multi-service product into an air-gapped sovereign cloud environment**.

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

| Integration Point | Test Type | Validates |
|---|---|---|
| `ocm add cv` with `component-constructor.yaml` | Unit | Component construction, resource bundling |
| `ocm sign` / `ocm verify` | Unit | Signing workflow, key handling |
| `ocm transfer ctf --copy-resources` | Unit | Resource localization, self-contained archive |
| `ocm transfer cv --verify` | Integration | Signature verification during transfer |
| `Repository` CR reconciliation | Integration | Controller validates registry connection |
| `Component` CR reconciliation | Integration | Controller fetches from registry, verifies signature |
| `Resource` CR with `referencePath` | Integration | Component reference traversal |
| `Resource` CR with `additionalStatusFields` | Integration | CEL expression evaluation for image extraction |
| `Deployer` CR with RGD | Integration | RGD instantiation and FluxCD resource creation |
| Upgrade detection (semver constraint) | Integration | Version bump triggers reconciliation |
| ORD configuration endpoint | Integration | `.well-known/open-resource-discovery` returns valid config |
| ORD document endpoint | Integration | ORD document describes APIs, events, dependencies |
| End-to-end air-gap flow | E2E | Full scenario from build to running workload |

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

## 12. Open Resource Discovery (ORD) Integration

[Open Resource Discovery (ORD)](https://open-resource-discovery.org/) is a Linux Foundation Europe protocol that enables applications to self-describe their exposed resources and capabilities. This integration demonstrates how sovereign-notes exposes its APIs via ORD for discovery by catalogs and marketplaces.

**Reference Implementation:** [ORD Reference Application](https://ord-reference-application.cfapps.sap.hana.ondemand.com/)

### 12.1 Architecture Overview

<details open>
<summary>Diagram: ORD Architecture</summary>

```mermaid
flowchart TB
    subgraph provider["Service Provider"]
        subgraph app["sovereign-notes"]
            api[Notes API]
            ord_config[well-known endpoint]
            ord_doc[ORD Document]
            api --> ord_doc
        end
        ocm_comp[OCM Component]
        app --> ocm_comp
    end

    subgraph aggregator["ORD Aggregator"]
        crawler[ORD Crawler]
        catalog[Service Catalog]
        ord_config --> crawler
        ord_doc --> crawler
        crawler --> catalog
    end

    subgraph consumer["Consumer"]
        discovery[Service Discovery]
        deploy[OCM Deployment]
        catalog --> discovery
        discovery --> deploy
        ocm_comp --> deploy
    end

    deploy --> k8s[Kubernetes Workloads]
```

</details>

### 12.2 ORD Configuration Endpoint

The sovereign-notes service exposes an ORD configuration at the well-known endpoint:

<details>
<summary>ORD configuration response</summary>

```json
// GET /.well-known/open-resource-discovery
{
  "openResourceDiscoveryV1": {
    "documents": [
      {
        "url": "/open-resource-discovery/v1/documents/1",
        "accessStrategies": [
          {
            "type": "open"
          }
        ],
        "perspective": "system-version"
      }
    ]
  }
}
```

</details>

### 12.3 ORD Document: Describing sovereign-notes

The ORD document describes the service's APIs, events, and metadata:

<details>
<summary>ORD document</summary>

```json
// GET /open-resource-discovery/v1/documents/1
{
  "$schema": "https://open-resource-discovery.org/spec-v1/interfaces/Document.schema.json",
  "openResourceDiscovery": "1.9",
  "policyLevel": "none",
  "describedSystemVersion": "1.0.0",

  "products": [
    {
      "ordId": "acme:product:sovereign-notes:",
      "title": "Sovereign Notes",
      "shortDescription": "A minimal notes API backed by PostgreSQL",
      "vendor": "acme:vendor:Acme:"
    }
  ],

  "packages": [
    {
      "ordId": "acme:package:sovereign-notes-api:v1",
      "title": "Sovereign Notes API Package",
      "version": "1.0.0",
      "partOfProducts": ["acme:product:sovereign-notes:"],
      "vendor": "acme:vendor:Acme:",
      "policyLevel": "none",
      "labels": {
        "ocm:component": "acme.org/sovereign/notes",
        "ocm:version": "1.0.0"
      }
    }
  ],

  "apiResources": [
    {
      "ordId": "acme:apiResource:sovereign-notes-api:v1",
      "title": "Notes REST API",
      "shortDescription": "CRUD operations for notes",
      "version": "1.0.0",
      "visibility": "public",
      "releaseStatus": "active",
      "partOfPackage": "acme:package:sovereign-notes-api:v1",
      "partOfConsumptionBundles": [
        {
          "ordId": "acme:consumptionBundle:sovereign-notes-public:v1"
        }
      ],
      "apiProtocol": "rest",
      "resourceDefinitions": [
        {
          "type": "openapi-v3",
          "mediaType": "application/json",
          "url": "/api/v1/openapi.json",
          "accessStrategies": [
            {
              "type": "open"
            }
          ]
        }
      ],
      "entryPoints": [
        "/notes"
      ],
      "extensible": {
        "supported": "no"
      }
    }
  ],

  "eventResources": [
    {
      "ordId": "acme:eventResource:sovereign-notes-events:v1",
      "title": "Notes Events",
      "shortDescription": "Events emitted when notes are created, updated, or deleted",
      "version": "1.0.0",
      "releaseStatus": "beta",
      "partOfPackage": "acme:package:sovereign-notes-api:v1",
      "resourceDefinitions": [
        {
          "type": "asyncapi-v2",
          "mediaType": "application/json",
          "url": "/api/v1/asyncapi.json",
          "accessStrategies": [
            {
              "type": "open"
            }
          ]
        }
      ]
    }
  ],

  "consumptionBundles": [
    {
      "ordId": "acme:consumptionBundle:sovereign-notes-public:v1",
      "version": "1.0.0",
      "title": "Sovereign Notes Public APIs",
      "shortDescription": "Public APIs for notes management",
      "credentialExchangeStrategies": [
        {
          "type": "custom",
          "customType": "acme:credential-exchange:api-key:v1",
          "customDescription": "API key authentication via X-API-Key header"
        }
      ]
    }
  ],

  "integrationDependencies": [
    {
      "ordId": "acme:integrationDependency:postgresql:v1",
      "title": "PostgreSQL Database",
      "shortDescription": "Required PostgreSQL database for persistence",
      "version": "1.0.0",
      "partOfPackage": "acme:package:sovereign-notes-api:v1",
      "mandatory": true,
      "aspects": [
        {
          "title": "Database Connection",
          "description": "PostgreSQL 16+ with notes database"
        }
      ]
    }
  ]
}
```

</details>

### 12.4 OCM Component with ORD Resource

The ORD document is bundled as a resource in the OCM component:

<details>
<summary><code>component-constructor.yaml</code> — ORD resource</summary>

```yaml
# components/notes/component-constructor.yaml (extended)
components:
  - name: acme.org/sovereign/notes
    version: "${VERSION}"
    provider:
      name: acme.org
    resources:
      # ... existing resources (image, helm-chart, rgd) ...

      # ORD Document for service discovery
      - name: ord-document
        type: blob
        relation: local
        input:
          type: file
          path: ./deploy/ord/document.json
          mediaType: application/json
        labels:
          - name: open-resource-discovery.org/version
            value: "1.9"
          - name: open-resource-discovery.org/perspective
            value: "system-version"
```

</details>

### 12.5 ORD Aggregator Integration

An ORD aggregator collects metadata from multiple providers:

<details open>
<summary>Diagram: ORD Aggregator</summary>

```mermaid
flowchart LR
    subgraph providers["ORD Providers"]
        notes[sovereign-notes]
        postgres[PostgreSQL]
        other[Other Services]
    end

    subgraph aggregator["ORD Aggregator"]
        crawler[Crawler]
        store[Metadata Store]
        api[Catalog API]
        crawler --> store
        store --> api
    end

    notes -->|/.well-known/ord| crawler
    postgres -->|/.well-known/ord| crawler
    other -->|/.well-known/ord| crawler

    subgraph consumers["Consumers"]
        dev[Developer Portal]
        automation[Automation Tools]
        ide[IDE Extensions]
    end

    api --> dev
    api --> automation
    api --> ide
```

</details>

### 12.6 Deployment with ORD Metadata

The OCM controller can expose ORD metadata from deployed components:

<details>
<summary><code>deploy/ord-service.yaml</code></summary>

```yaml
# deploy/ord-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: sovereign-notes-ord
  namespace: sovereign-product
  labels:
    open-resource-discovery.org/provider: "true"
  annotations:
    # ORD aggregators can discover this service
    open-resource-discovery.org/base-url: "http://sovereign-notes.sovereign-product.svc:8080"
spec:
  selector:
    app: sovereign-notes
  ports:
    - name: http
      port: 8080
      targetPort: 8080
```

</details>

### 12.7 sovereign-notes ORD Implementation

Add ORD endpoints to the sovereign-notes application:

<details>
<summary><code>cmd/sovereign-notes/ord.go</code></summary>

```go
// cmd/sovereign-notes/ord.go
package main

import (
    "encoding/json"
    "net/http"
)

func registerORDEndpoints() {
    // ORD Configuration endpoint
    http.HandleFunc("/.well-known/open-resource-discovery", func(w http.ResponseWriter, r *http.Request) {
        config := map[string]interface{}{
            "openResourceDiscoveryV1": map[string]interface{}{
                "documents": []map[string]interface{}{
                    {
                        "url": "/open-resource-discovery/v1/documents/1",
                        "accessStrategies": []map[string]string{
                            {"type": "open"},
                        },
                        "perspective": "system-version",
                    },
                },
            },
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(config)
    })

    // ORD Document endpoint
    http.HandleFunc("/open-resource-discovery/v1/documents/1", func(w http.ResponseWriter, r *http.Request) {
        // Serve the ORD document (loaded from embedded file or generated)
        w.Header().Set("Content-Type", "application/json")
        http.ServeFile(w, r, "/etc/ord/document.json")
    })

    // OpenAPI specification
    http.HandleFunc("/api/v1/openapi.json", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        http.ServeFile(w, r, "/etc/openapi/spec.json")
    })
}
```

</details>

### 12.8 OpenAPI Specification

<details>
<summary><code>components/notes/deploy/openapi/spec.yaml</code></summary>

```yaml
# components/notes/deploy/openapi/spec.yaml
openapi: "3.0.3"
info:
  title: Sovereign Notes API
  version: "1.0.0"
  description: A minimal notes API backed by PostgreSQL
  contact:
    name: Acme Corp
    url: https://acme.org
  license:
    name: Apache 2.0
    url: https://www.apache.org/licenses/LICENSE-2.0

servers:
  - url: /
    description: Current instance

paths:
  /notes:
    get:
      summary: List all notes
      operationId: listNotes
      responses:
        "200":
          description: List of notes
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: "#/components/schemas/Note"
    post:
      summary: Create a note
      operationId: createNote
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/CreateNote"
      responses:
        "201":
          description: Note created
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Note"

  /notes/{id}:
    get:
      summary: Get a note by ID
      operationId: getNote
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: integer
      responses:
        "200":
          description: Note found
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Note"
        "404":
          description: Note not found
    delete:
      summary: Delete a note
      operationId: deleteNote
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: integer
      responses:
        "204":
          description: Note deleted
        "404":
          description: Note not found

  /healthz:
    get:
      summary: Liveness probe
      operationId: healthz
      responses:
        "200":
          description: Service is alive

  /readyz:
    get:
      summary: Readiness probe
      operationId: readyz
      responses:
        "200":
          description: Service is ready
        "503":
          description: Service not ready (database unavailable)

components:
  schemas:
    Note:
      type: object
      properties:
        id:
          type: integer
        content:
          type: string
        created_at:
          type: string
          format: date-time
      required:
        - id
        - content
        - created_at
    CreateNote:
      type: object
      properties:
        content:
          type: string
      required:
        - content
```

</details>

### 12.9 Integration with Service Catalogs

ORD enables integration with various service discovery systems:

| System             | Integration Pattern                               |
|--------------------|---------------------------------------------------|
| **Backstage**      | ORD plugin fetches metadata into software catalog |
| **Port**           | ORD aggregator populates service blueprints       |
| **OpsLevel**       | Import services via ORD document sync             |
| **Custom Catalog** | Direct ORD API consumption                        |

This enables the sovereign-notes service to be discovered and documented automatically across enterprise service catalogs without manual registration

---

## 13. OpenMCP Integration (Multi-Control-Plane Deployment)

[OpenMCP (Open Managed Control Plane)](https://openmcp-project.github.io/docs/) is part of the [ApeiroRA](https://apeirora.eu/) ecosystem that enables Infrastructure- and Configuration-as-Data capabilities as a Service. This integration demonstrates deploying sovereign-notes across multiple managed control planes for multi-tenant or multi-environment scenarios.

**See also:** [Section 14: Platform Mesh Integration](#14-platform-mesh-integration-service-ordering-api) for service ordering and discovery capabilities that build on top of OpenMCP control planes.

### 13.1 Sovereign Delivery Model: Global vs Local Control Planes

In sovereign cloud scenarios, control planes map to two deployment patterns:

| Control Plane Type | Location | Connectivity | Purpose |
|--------------------|----------|--------------|---------|
| **Global Control Plane** | Vendor/Central Data Center | Connected | Component management, signing, orchestration |
| **Local Control Plane** | Sovereign Environment | Air-gapped | Isolated workload execution, local state |

<details open>
<summary>Diagram: Global vs Local Control Planes</summary>

```mermaid
flowchart TB
    subgraph vendor["Vendor Environment (Connected)"]
        subgraph global_mcp["Global Control Plane (MCP)"]
            ocm_global[OCM Controller]
            comp_mgmt[Component Management]
            signing[Signing Service]
            orch[Deployment Orchestration]
        end
        registry[(OCM Registry)]
    end

    subgraph transfer["Air-Gap Transfer"]
        ctf[(CTF Archive)]
    end

    subgraph sovereign1["Sovereign Cloud A (Air-Gapped)"]
        subgraph local_mcp1["Local Control Plane (MCP)"]
            ocm_local1[OCM Controller]
            flux1[FluxCD]
            kro1[kro]
        end
        local_reg1[(Local Registry)]
        workloads1[Workloads]
    end

    subgraph sovereign2["Sovereign Cloud B (Air-Gapped)"]
        subgraph local_mcp2["Local Control Plane (MCP)"]
            ocm_local2[OCM Controller]
            flux2[FluxCD]
            kro2[kro]
        end
        local_reg2[(Local Registry)]
        workloads2[Workloads]
    end

    ocm_global --> registry
    registry --> ctf
    ctf --> local_reg1
    ctf --> local_reg2
    local_reg1 --> ocm_local1
    local_reg2 --> ocm_local2
    ocm_local1 --> flux1 --> workloads1
    ocm_local2 --> flux2 --> workloads2
```

</details>

**Global Control Plane Responsibilities:**
- Maintains authoritative component versions and signatures
- Orchestrates component builds and releases
- Prepares CTF archives for air-gap transfer
- Provides deployment templates (RGDs) for sovereign environments

**Local Control Plane Responsibilities:**
- Operates fully disconnected from global infrastructure
- Verifies component signatures using pre-distributed public keys
- Executes deployments using local registry mirrors
- Maintains local state and handles upgrades autonomously

### 13.2 Architecture Overview

<details open>
<summary>Diagram: OpenMCP Architecture</summary>

```mermaid
flowchart TB
    subgraph openmcp["OpenMCP Platform"]
        subgraph project["Project: Acme"]
            subgraph ws_global["Workspace: Global (Connected)"]
                mcp_global[ManagedControlPlane: global]
            end
            subgraph ws_sovereign_a["Workspace: Sovereign-A"]
                mcp_sov_a[ManagedControlPlane: sovereign-a]
            end
            subgraph ws_sovereign_b["Workspace: Sovereign-B"]
                mcp_sov_b[ManagedControlPlane: sovereign-b]
            end
        end
    end

    subgraph ocm["OCM Layer (Global)"]
        repo[Repository]
        comp[Component]
        repo --> comp
    end

    subgraph transfer["Air-Gap Transfer"]
        ctf_a[(CTF Archive A)]
        ctf_b[(CTF Archive B)]
    end

    comp --> ctf_a
    comp --> ctf_b
    ctf_a --> mcp_sov_a
    ctf_b --> mcp_sov_b

    mcp_global --> |orchestrates| ctf_a
    mcp_global --> |orchestrates| ctf_b

    mcp_sov_a --> workload_a[sovereign-notes A]
    mcp_sov_b --> workload_b[sovereign-notes B]
```

</details>

### 13.3 OpenMCP Resource Hierarchy

OpenMCP organizes resources in a hierarchy that maps to the sovereign delivery model:

<details>
<summary>OpenMCP Project and Workspace definitions</summary>

```yaml
# Project represents an organization or team
apiVersion: core.openmcp.cloud/v1alpha1
kind: Project
metadata:
  name: acme
spec:
  displayName: "Acme Corporation"
  description: "Sovereign cloud deployment project"
---
# Global Workspace - connected environment for component management
apiVersion: core.openmcp.cloud/v1alpha1
kind: Workspace
metadata:
  name: global
  namespace: project-acme
spec:
  project: acme
  displayName: "Global Control Plane (Connected)"
  labels:
    delivery.ocm.software/connectivity: connected
---
# Sovereign Workspace A - air-gapped environment
apiVersion: core.openmcp.cloud/v1alpha1
kind: Workspace
metadata:
  name: sovereign-a
  namespace: project-acme
spec:
  project: acme
  displayName: "Sovereign Cloud A (Air-Gapped)"
  labels:
    delivery.ocm.software/connectivity: airgapped
    delivery.ocm.software/region: eu-central
---
# Sovereign Workspace B - air-gapped environment
apiVersion: core.openmcp.cloud/v1alpha1
kind: Workspace
metadata:
  name: sovereign-b
  namespace: project-acme
spec:
  project: acme
  displayName: "Sovereign Cloud B (Air-Gapped)"
  labels:
    delivery.ocm.software/connectivity: airgapped
    delivery.ocm.software/region: eu-west
```

</details>

### 13.4 Global Control Plane Configuration

The **Global Control Plane** runs in a connected environment and manages component lifecycle:

<details>
<summary><code>openmcp/mcp-global.yaml</code></summary>

```yaml
# openmcp/mcp-global.yaml
apiVersion: core.openmcp.cloud/v2alpha1
kind: ManagedControlPlaneV2
metadata:
  name: global-control-plane
  namespace: workspace-global
  labels:
    delivery.ocm.software/role: global
spec:
  displayName: "Global Control Plane"

  authentication:
    oidc:
      issuerURL: https://auth.acme.org
      clientID: global-control-plane
    serviceAccounts:
      - name: ocm-controller
        namespace: ocm-system

  authorization:
    clusterRoleBindings:
      - roleRef:
          apiGroup: rbac.authorization.k8s.io
          kind: ClusterRole
          name: cluster-admin
        subjects:
          - kind: Group
            name: acme-platform-team
            apiGroup: rbac.authorization.k8s.io

  # Global control plane components
  components:
    flux:
      enabled: true
    kro:
      enabled: true
    # OCM controller for component management
    ocmController:
      enabled: true
      config:
        # Connected to external OCM registry
        registryUrl: ghcr.io/open-component-model/reference-scenario
        signing:
          enabled: true
          keyRef:
            secretName: acme-signing-key
```

</details>

### 13.5 Local Control Plane Configuration (Sovereign/Air-Gapped)

**Local Control Planes** run within each sovereign environment, fully isolated:

<details>
<summary><code>openmcp/mcp-sovereign-a.yaml</code> and <code>openmcp/mcp-sovereign-b.yaml</code></summary>

```yaml
# openmcp/mcp-sovereign-a.yaml
apiVersion: core.openmcp.cloud/v2alpha1
kind: ManagedControlPlaneV2
metadata:
  name: sovereign-a-control-plane
  namespace: workspace-sovereign-a
  labels:
    delivery.ocm.software/role: local
    delivery.ocm.software/region: eu-central
spec:
  displayName: "Sovereign Cloud A - Local Control Plane"

  # Local authentication (sovereign IdP)
  authentication:
    oidc:
      issuerURL: https://auth.sovereign-a.local
      clientID: sovereign-a-control-plane
    serviceAccounts:
      - name: ocm-controller
        namespace: ocm-system

  authorization:
    clusterRoleBindings:
      - roleRef:
          apiGroup: rbac.authorization.k8s.io
          kind: ClusterRole
          name: cluster-admin
        subjects:
          - kind: Group
            name: sovereign-a-admins
            apiGroup: rbac.authorization.k8s.io

  # Local control plane components
  components:
    flux:
      enabled: true
    kro:
      enabled: true
    # OCM controller configured for air-gapped operation
    ocmController:
      enabled: true
      config:
        # Local registry mirror (air-gapped)
        registryUrl: registry.sovereign-a.local:5000
        # Signature verification only (no signing)
        verify:
          enabled: true
          publicKeyRef:
            secretName: acme-public-key

  # Production-grade settings for sovereign environment
  highAvailability:
    enabled: true
    replicas: 3

  # Network isolation
  networking:
    isolated: true
    allowedEgress: []  # No external network access
---
# openmcp/mcp-sovereign-b.yaml
apiVersion: core.openmcp.cloud/v2alpha1
kind: ManagedControlPlaneV2
metadata:
  name: sovereign-b-control-plane
  namespace: workspace-sovereign-b
  labels:
    delivery.ocm.software/role: local
    delivery.ocm.software/region: eu-west
spec:
  displayName: "Sovereign Cloud B - Local Control Plane"

  authentication:
    oidc:
      issuerURL: https://auth.sovereign-b.local
      clientID: sovereign-b-control-plane
    serviceAccounts:
      - name: ocm-controller
        namespace: ocm-system

  authorization:
    clusterRoleBindings:
      - roleRef:
          apiGroup: rbac.authorization.k8s.io
          kind: ClusterRole
          name: cluster-admin
        subjects:
          - kind: Group
            name: sovereign-b-admins
            apiGroup: rbac.authorization.k8s.io

  components:
    flux:
      enabled: true
    kro:
      enabled: true
    ocmController:
      enabled: true
      config:
        registryUrl: registry.sovereign-b.local:5000
        verify:
          enabled: true
          publicKeyRef:
            secretName: acme-public-key

  highAvailability:
    enabled: true
    replicas: 3

  networking:
    isolated: true
    allowedEgress: []
```

</details>

### 13.6 Global Control Plane: Component Management

The **Global Control Plane** manages component lifecycle in the connected environment:

<details>
<summary><code>deploy/global/repository.yaml</code> and <code>deploy/global/component.yaml</code></summary>

```yaml
# deploy/global/repository.yaml - Global MCP
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: acme-components
  namespace: ocm-system
spec:
  repositorySpec:
    baseUrl: ghcr.io/open-component-model/reference-scenario
    type: OCIRegistry
  interval: 10m
---
# deploy/global/component.yaml - Global MCP
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: sovereign-product
  namespace: ocm-system
spec:
  component: acme.org/sovereign/product
  repositoryRef:
    name: acme-components
  semver: ">=1.0.0"
  interval: 10m
  # Sign components for distribution
  sign:
    - signature: acme-signature
      secretRef:
        name: acme-signing-key
```

</details>

### 13.7 Air-Gap Transfer: Global to Local

Components are transferred from Global to Local control planes via CTF archives:

<details>
<summary>Air-gap transfer Job</summary>

```yaml
# Transfer job executed on Global Control Plane
apiVersion: batch/v1
kind: Job
metadata:
  name: transfer-to-sovereign-a
  namespace: ocm-system
spec:
  template:
    spec:
      containers:
        - name: ocm-transfer
          image: ghcr.io/open-component-model/ocm-cli:latest
          command:
            - /bin/sh
            - -c
            - |
              # Export component to CTF archive
              ocm transfer cv \
                --recursive \
                --copy-resources \
                --verify acme-signature=/keys/acme-public.pem \
                ghcr.io/open-component-model/reference-scenario//acme.org/sovereign/product:1.0.0 \
                /export/sovereign-a-ctf

              # Archive is then physically transferred to sovereign environment
          volumeMounts:
            - name: export
              mountPath: /export
            - name: keys
              mountPath: /keys
              readOnly: true
      volumes:
        - name: export
          persistentVolumeClaim:
            claimName: ctf-export-pvc
        - name: keys
          secret:
            secretName: acme-public-key
      restartPolicy: OnFailure
```

</details>

### 13.8 Local Control Plane: Deployment

The **Local Control Plane** deploys components from the local registry mirror:

<details>
<summary><code>deploy/local/repository.yaml</code> and <code>deploy/local/component.yaml</code></summary>

```yaml
# deploy/local/repository.yaml - Sovereign-A Local MCP
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: sovereign-repo
  namespace: ocm-system
spec:
  repositorySpec:
    baseUrl: registry.sovereign-a.local:5000  # Air-gapped registry
    type: OCIRegistry
  interval: 10m
---
# deploy/local/component.yaml - Sovereign-A Local MCP
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
  # Verify signatures (no signing capability in air-gapped env)
  verify:
    - signature: acme-signature
      secretRef:
        name: acme-public-key
```

</details>

### 13.9 Environment-Specific RGD Instances

Configure different values per Local Control Plane:

<details>
<summary><code>deploy/local/sovereign-a-instance.yaml</code> and <code>deploy/local/sovereign-b-instance.yaml</code></summary>

```yaml
# deploy/local/sovereign-a-instance.yaml
apiVersion: kro.run/v1alpha1
kind: SovereignProduct
metadata:
  name: sovereign-product
  namespace: ocm-system
  labels:
    delivery.ocm.software/mcp: sovereign-a-control-plane
    delivery.ocm.software/role: local
spec:
  namespace: sovereign-product
  notes:
    replicas: 3  # HA for sovereign environment
  postgres:
    storageSize: "50Gi"
    storageClass: "sovereign-ssd"
---
# deploy/local/sovereign-b-instance.yaml
apiVersion: kro.run/v1alpha1
kind: SovereignProduct
metadata:
  name: sovereign-product
  namespace: ocm-system
  labels:
    delivery.ocm.software/mcp: sovereign-b-control-plane
    delivery.ocm.software/role: local
spec:
  namespace: sovereign-product
  notes:
    replicas: 5  # HA for production
  postgres:
    storageSize: "100Gi"
    storageClass: "fast-ssd"
```

</details>

### 13.10 Global-to-Local Deployment Flow

<details open>
<summary>Diagram: Global-to-Local Deployment Flow</summary>

```mermaid
sequenceDiagram
    participant Dev as Developer
    participant Global as Global Control Plane
    participant Registry as OCM Registry
    participant CTF as CTF Archive
    participant LocalA as Local MCP: Sovereign-A
    participant LocalB as Local MCP: Sovereign-B

    Dev->>Global: Push signed component v1.1.0
    Global->>Registry: Store component + signature

    Note over Global: Prepare for air-gap transfer
    Global->>CTF: Export component to CTF archive
    Global->>CTF: Include public key + RGD templates

    Note over CTF: Physical transfer to sovereign environments
    CTF-->>LocalA: Transfer via secure channel
    CTF-->>LocalB: Transfer via secure channel

    Note over LocalA: Sovereign-A deployment (air-gapped)
    LocalA->>LocalA: Import CTF to local registry
    LocalA->>LocalA: Verify signature with public key
    LocalA->>LocalA: Deploy via RGD (3 replicas)

    Note over LocalB: Sovereign-B deployment (air-gapped)
    LocalB->>LocalB: Import CTF to local registry
    LocalB->>LocalB: Verify signature with public key
    LocalB->>LocalB: Deploy via RGD (5 replicas)
```

</details>

### 13.11 Cross-MCP Service Discovery (via ORD)

Services deployed across Local MCPs can be discovered via ORD. Each sovereign environment exposes its own ORD endpoint:

<details>
<summary>Cross-MCP Service Discovery configuration</summary>

```yaml
# Sovereign-A: Service with ORD metadata
apiVersion: v1
kind: Service
metadata:
  name: sovereign-notes
  namespace: sovereign-product
  annotations:
    # Local ORD endpoint (accessible within sovereign environment)
    open-resource-discovery.org/base-url: "https://notes.sovereign-a.local"
    delivery.ocm.software/mcp: "sovereign-a-control-plane"
    delivery.ocm.software/role: "local"
    delivery.ocm.software/region: "eu-central"
spec:
  selector:
    app: sovereign-notes
  ports:
    - port: 8080
---
# Sovereign-B: Service with ORD metadata
apiVersion: v1
kind: Service
metadata:
  name: sovereign-notes
  namespace: sovereign-product
  annotations:
    open-resource-discovery.org/base-url: "https://notes.sovereign-b.local"
    delivery.ocm.software/mcp: "sovereign-b-control-plane"
    delivery.ocm.software/role: "local"
    delivery.ocm.software/region: "eu-west"
spec:
  selector:
    app: sovereign-notes
  ports:
    - port: 8080
```

</details>

### 13.12 Benefits of Global/Local Control Plane Architecture

| Benefit | Global Control Plane | Local Control Plane |
|---------|---------------------|---------------------|
| **Connectivity** | Connected to external registries | Fully air-gapped |
| **Signing** | Creates and manages signatures | Verifies signatures only |
| **Component Source** | External OCM registry | Local registry mirror |
| **Orchestration** | Prepares CTF archives for distribution | Executes deployments autonomously |
| **State** | Authoritative version management | Independent local state |
| **Updates** | Initiates version updates | Receives updates via CTF transfer |
| **IdP** | Vendor/central authentication | Sovereign-local authentication |

### 13.13 Summary: MCP Mapping to Sovereign Delivery

| Concept | OpenMCP Resource | Sovereign Delivery Role |
|---------|------------------|------------------------|
| Vendor/Central Environment | Global Workspace + MCP | Component management, signing, CTF export |
| Air-Gapped Environment | Sovereign Workspace + MCP | Isolated deployment, signature verification |
| Project | Acme Project | Organizational boundary |
| Workspace (Connected) | `workspace-global` | Houses global control plane |
| Workspace (Air-Gapped) | `workspace-sovereign-*` | Houses local control planes |
| MCP (Global) | `global-control-plane` | Manages authoritative component state |
| MCP (Local) | `sovereign-*-control-plane` | Executes deployments in isolation |

---

## 14. Platform Mesh Integration (Service Ordering API)

[Platform Mesh](https://platform-mesh.io/) is a Linux Foundation Europe initiative that enables service discovery, ordering, and orchestration across providers using Kubernetes Resource Model (KRM). This integration demonstrates how OCM components can be offered as services in a multi-tenant marketplace.

**Integration with OpenMCP (Section 13):** Platform Mesh operates at the service ordering layer, while OpenMCP manages the underlying control plane infrastructure. In our sovereign delivery model:
- **Global Control Plane** hosts the Platform Mesh service catalog and handles service ordering
- **Local Control Planes** receive deployed service instances after air-gap transfer
- Platform Mesh creates OCM CRs (Component, Resource, RGD instances) that flow through the OpenMCP deployment pipeline

### 14.1 Architecture Overview

<details open>
<summary>Diagram: Platform Mesh with OpenMCP Integration</summary>

```mermaid
flowchart TB
    subgraph global["Global Control Plane (OpenMCP)"]
        subgraph provider["Service Provider"]
            ocm_comp[OCM Component]
            offering[ServiceOffering]
            export[Export]
            ocm_comp --> offering
            offering --> export
        end

        subgraph mesh["Platform Mesh"]
            catalog[Service Catalog]
            binding[ServiceBinding]
            export --> catalog
            catalog --> binding
        end

        subgraph consumer["Consumer Workspace"]
            order[ServiceOrder]
            instance[ServiceInstance]
            deployer[OCM Deployer]
            binding --> order
            order --> instance
            instance --> deployer
        end
    end

    deployer --> ctf[CTF Archive]
    ctf -->|Air-Gap| local

    subgraph local["Local Control Plane (Sovereign)"]
        transfer[ocm transfer]
        local_reg[(Local Registry)]
        local_ocm[OCM Controller]
        k8s[Kubernetes Workloads]

        transfer --> local_reg
        local_reg --> local_ocm
        local_ocm --> k8s
    end
```

</details>

### 14.2 Service Provider: Exporting sovereign-notes

The service provider publishes the OCM component as a ServiceOffering in the Global Control Plane:

<details>
<summary>platform-mesh/service-offering.yaml</summary>

```yaml
# platform-mesh/service-offering.yaml
apiVersion: platform-mesh.io/v1alpha1
kind: ServiceOffering
metadata:
  name: sovereign-notes
  namespace: service-catalog
  labels:
    openmcp.io/workspace: workspace-global  # OpenMCP workspace label
spec:
  displayName: "Sovereign Notes Service"
  description: "A minimal notes API backed by PostgreSQL, delivered as OCM components"
  version: "1.0.0"

  # Reference to the OCM component
  source:
    type: ocm
    ocm:
      component: acme.org/sovereign/product
      repository:
        url: ghcr.io/open-component-model/reference-scenario
      semver: ">=1.0.0"
      verify:
        - signature: acme-signature

  # Service configuration schema (exposed to consumers)
  schema:
    type: object
    properties:
      replicas:
        type: integer
        default: 2
        minimum: 1
        maximum: 10
        description: "Number of application replicas"
      storage:
        type: object
        properties:
          size:
            type: string
            default: "1Gi"
            description: "PostgreSQL storage size"
          storageClass:
            type: string
            default: ""
            description: "Storage class for persistence"

  # Dependencies on other services
  dependencies:
    - name: postgres
      serviceRef:
        name: postgresql
        namespace: service-catalog
      optional: false

  # Deployment target specification
  deployment:
    controlPlaneSelector:
      matchLabels:
        openmcp.io/type: sovereign  # Target sovereign control planes
---
# Export the service to the mesh catalog
apiVersion: platform-mesh.io/v1alpha1
kind: Export
metadata:
  name: sovereign-notes-export
  namespace: service-catalog
spec:
  serviceOfferingRef:
    name: sovereign-notes
  visibility: public
  allowedConsumers:
    - workspace: "*"  # Available to all workspaces
```

</details>

### 14.3 Service Consumer: Ordering sovereign-notes

Consumers discover and order services from the catalog. The ServiceOrder triggers the complete deployment pipeline:

<details>
<summary>platform-mesh/service-order.yaml</summary>

```yaml
# platform-mesh/service-order.yaml
apiVersion: platform-mesh.io/v1alpha1
kind: ServiceOrder
metadata:
  name: my-notes-instance
  namespace: consumer-workspace
  labels:
    openmcp.io/workspace: workspace-global
spec:
  serviceOfferingRef:
    name: sovereign-notes
    namespace: service-catalog

  # Consumer-provided configuration
  parameters:
    replicas: 3
    storage:
      size: "10Gi"
      storageClass: "fast-ssd"

  # Target deployment environment
  target:
    controlPlane: sovereign-germany-control-plane  # OpenMCP target
    namespace: my-application

  # Air-gap transfer configuration
  transfer:
    method: ctf
    verifySignatures: true
---
# ServiceInstance is created automatically by Platform Mesh
apiVersion: platform-mesh.io/v1alpha1
kind: ServiceInstance
metadata:
  name: my-notes-instance
  namespace: consumer-workspace
status:
  phase: Running
  controlPlane: sovereign-germany-control-plane
  endpoints:
    - name: api
      url: "http://sovereign-notes.my-application.svc:8080"
  credentials:
    secretRef:
      name: my-notes-instance-credentials
```

</details>

### 14.4 Dependency Ordering

Platform Mesh handles service dependencies automatically. When `sovereign-notes` is ordered, it:

1. Checks if PostgreSQL dependency is satisfied
2. If not, automatically orders PostgreSQL first
3. Waits for PostgreSQL to be `Running`
4. Then provisions sovereign-notes with DATABASE_URL injected

<details>
<summary>platform-mesh/dependency-graph.yaml</summary>

```yaml
# platform-mesh/dependency-graph.yaml
apiVersion: platform-mesh.io/v1alpha1
kind: ServiceDependencyGraph
metadata:
  name: my-notes-dependencies
  namespace: consumer-workspace
spec:
  root: my-notes-instance
status:
  dependencies:
    - name: my-notes-instance
      status: Running
      controlPlane: sovereign-germany-control-plane
      dependsOn:
        - name: postgres-instance
          status: Running
          controlPlane: sovereign-germany-control-plane
          dependsOn: []
```

</details>

### 14.5 Integration with OCM Controller

Platform Mesh integrates with OCM by creating the necessary CRs when a ServiceOrder is fulfilled. These CRs are then synced to the target Local Control Plane via OpenMCP:

<details>
<summary>Generated OCM Resources</summary>

```yaml
# Generated by Platform Mesh when ServiceOrder is approved
# Synced to Local Control Plane via OpenMCP
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: my-notes-instance
  namespace: consumer-workspace
  labels:
    platform-mesh.io/service-instance: my-notes-instance
    openmcp.io/source-workspace: workspace-global
    openmcp.io/target-control-plane: sovereign-germany-control-plane
spec:
  component: acme.org/sovereign/product
  repositoryRef:
    name: provider-repo  # Created by Platform Mesh binding
  semver: ">=1.0.0"
  interval: 10m
  verify:
    - signature: acme-signature
      secretRef:
        name: provider-signing-key
---
# RGD Instance with consumer parameters
apiVersion: kro.run/v1alpha1
kind: SovereignProduct
metadata:
  name: my-notes-instance
  namespace: consumer-workspace
  labels:
    platform-mesh.io/service-instance: my-notes-instance
spec:
  namespace: my-application
  notes:
    replicas: 3  # From ServiceOrder parameters
  postgres:
    storageSize: "10Gi"
    storageClass: "fast-ssd"
```

</details>

### 14.6 Multi-Provider Scenarios

Platform Mesh enables cross-provider service composition across multiple OpenMCP workspaces:

<details open>
<summary>Diagram: Multi-Provider with OpenMCP</summary>

```mermaid
flowchart LR
    subgraph global["Global Control Plane"]
        subgraph provider1["Provider A Workspace"]
            notes[sovereign-notes]
        end

        subgraph provider2["Provider B Workspace"]
            postgres[Managed PostgreSQL]
        end

        subgraph mesh["Platform Mesh Catalog"]
            cat_notes[Notes Offering]
            cat_pg[PostgreSQL Offering]
        end

        subgraph consumer["Consumer Workspace"]
            order[ServiceOrder]
        end
    end

    notes --> cat_notes
    postgres --> cat_pg
    cat_notes -.->|depends on| cat_pg
    order --> cat_notes
    order --> cat_pg

    subgraph local["Local Control Planes"]
        local1[sovereign-germany]
        local2[sovereign-france]
    end

    cat_notes --> local1
    cat_notes --> local2
    cat_pg --> local1
    cat_pg --> local2
```

</details>

This allows consumers to:
- Use managed PostgreSQL from Provider B instead of self-hosted
- Mix and match services from different providers
- Maintain consistent ordering and dependency resolution
- Deploy to multiple sovereign environments via OpenMCP

### 14.7 End-to-End Flow: Platform Mesh → OpenMCP → OCM

The complete service delivery flow integrates all three systems:

<details open>
<summary>Diagram: Complete Integration Flow</summary>

```mermaid
sequenceDiagram
    participant Consumer
    participant PlatformMesh as Platform Mesh
    participant GlobalMCP as Global Control Plane
    participant CTF as CTF Archive
    participant LocalMCP as Local Control Plane
    participant OCM as OCM Controller
    participant K8s as Kubernetes

    Consumer->>PlatformMesh: Create ServiceOrder
    PlatformMesh->>PlatformMesh: Resolve dependencies
    PlatformMesh->>GlobalMCP: Create OCM CRs (Component, RGD)
    GlobalMCP->>GlobalMCP: Verify signatures
    GlobalMCP->>CTF: Export to CTF archive

    Note over CTF: Air-Gap Transfer

    CTF->>LocalMCP: Import CTF
    LocalMCP->>LocalMCP: Verify signatures
    LocalMCP->>OCM: Sync OCM CRs
    OCM->>OCM: Reconcile Component
    OCM->>OCM: Reconcile Resource
    OCM->>K8s: Deploy via FluxCD
    K8s-->>Consumer: Service Ready
```

</details>

| Step | System | Action |
|------|--------|--------|
| 1 | Platform Mesh | Consumer creates ServiceOrder with configuration |
| 2 | Platform Mesh | Resolve service dependencies (e.g., PostgreSQL) |
| 3 | Platform Mesh | Generate OCM CRs (Component, Resource, RGD Instance) |
| 4 | Global MCP | Verify component signatures in connected environment |
| 5 | Global MCP | Export components to CTF archive for air-gap transfer |
| 6 | Air-Gap | Physical or logical transfer of CTF to sovereign environment |
| 7 | Local MCP | Import and verify CTF contents |
| 8 | Local MCP | Sync OCM CRs to local OCM Controller |
| 9 | OCM Controller | Reconcile Component and Resource CRs |
| 10 | FluxCD | Deploy workloads to Kubernetes |

---

## 15. Deployment Extensibility

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

| Target            | How it integrates                                                       |
|-------------------|-------------------------------------------------------------------------|
| **FluxCD**        | RGD templates create `OCIRepository` + `HelmRelease` or `Kustomization` |
| **ArgoCD**        | RGD templates create `Application` CRs                                  |

The **component structure remains unchanged** — only the RGD templates vary per deployment target.

---

## 16. Key Design Decisions

| Decision                              | Rationale                                                                              |
|---------------------------------------|----------------------------------------------------------------------------------------|
| **Custom Go app (sovereign-notes)**   | Real PostgreSQL dependency, full build pipeline demo, tiny and understandable          |
| **RGD-based configuration**           | Strongly-typed values via schema, CEL expressions for image localization               |
| **RSA signing with verification**     | Meets sovereign cloud security requirements with own PKI, can be replaced with keyless |
| **kro + FluxCD deployment**           | RGDs provide flexibility; FluxCD is mature and widely deployed                         |
| **kind with local registry**          | Fully reproducible locally, simulates air-gap registry                                 |
| **semver constraint for upgrades**    | Controller auto-detects new versions without CR changes                                |
| **additionalStatusFields for images** | CEL expressions extract registry/repo/tag for localization without separate CR         |
| **ORD for service discovery**         | Decentralized metadata discovery; services self-describe via standard protocol         |
| **Global/Local Control Planes**       | Global MCP manages components in connected env; Local MCPs deploy in air-gapped isolation |
| **Platform Mesh for service ordering**| KRM-based service catalog with automatic dependency resolution; integrates with OpenMCP for multi-control-plane delivery |
