# Design: Reference Scenario — Sovereign Cloud Delivery

* **Status**: proposed
* **Deciders**: Gergely Brautigam, Fabian Burth, Jakob Moeller
* **Date**: 2025-02-06

**Issue:** [ocm-project#842](https://github.com/open-component-model/ocm-project/issues/842)  
**Parent Epic:** [ocm-project#424 — OCM Reference Scenario / Golden Path](https://github.com/open-component-model/ocm-project/issues/424)  
**Related:** [ocm-project#843 — Document and Describe Reference Scenario in "How To's"](https://github.com/open-component-model/ocm-project/issues/843)

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

### 2.2 Controller Reconciliation Detail

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

### 4.1 Component: `acme.org/sovereign/notes`

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

**ResourceGraphDefinition (`deploy/rgd.yaml`):**
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

### 4.2 Component: `acme.org/sovereign/postgres`

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

**ResourceGraphDefinition (`deploy/rgd.yaml`):**
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

### 4.3 Meta Component: `acme.org/sovereign/product`

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

**Base manifests (`deploy/base.yaml`):**
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

**Product RGD (`deploy/rgd.yaml`):**
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

### 4.4 Settings File

```yaml
# settings.yaml
VERSION: 1.0.0
POSTGRES_VERSION: "16-alpine"
```

Note: Environment-specific configuration (replicas, resources, storage) is now defined via RGD instance specs rather than component resources. Database credentials should be managed via Kubernetes secrets (e.g., ExternalSecrets, sealed-secrets) rather than embedded in components.

---

## 5. Signing Workflow

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

### 5.1 Key Generation (One-time Setup)

```bash
# Generate RSA key pair for signing
openssl genpkey -algorithm RSA -out keys/acme-private.pem -pkeyopt rsa_keygen_bits:4096
openssl rsa -pubout -in keys/acme-private.pem -out keys/acme-public.pem

# Store private key securely (CI secret, HSM, etc.)
# Public key is bundled in the product component
```

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

### 5.2 Sign During Build

```bash
# After building CTF, sign all component versions
ocm sign componentversion
```

### 5.3 Verify Before Transfer

_NOTE: We can currently only verify signatures for cvs already transferred or in archives. We need a transformer here to bridge this later._

```bash
# When transferring to air-gap, verify signatures
ocm verify cv ghcr.io/ocm/reference-scenario//acme.org/sovereign/product:${VERSION}
ocm transfer cv \
  --recursive \
  --copy-resources \
  ghcr.io/ocm/reference-scenario//acme.org/sovereign/product:${VERSION} \
  ./transport-archive
```

### 5.4 Verify on Cluster

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

---

## 6. Configuration via ResourceGraphDefinition

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

The key insight is that **configuration flows through ResourceGraphDefinition (RGD) schemas**, not separate config resources. This enables:
- Strongly-typed configuration via RGD schema definitions
- Image localization via `additionalStatusFields` CEL expressions
- Environment-specific values via RGD instance specs
- Air-gap friendly (images referenced through Resource status)

### 6.1 Resource with Image Extraction

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

### 6.2 ResourceGraphDefinition for Deployment

The RGD defines a custom schema and templates for FluxCD resources:

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

### 6.3 RGD Instance for Environment Configuration

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

---

## 7. Build & Publish Pipeline

### 7.1 Taskfile (Local Development)

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

### 7.2 GitHub Actions Pipeline

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

---

## 8. OCM Controller Deployment Manifests

### 8.1 Repository

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

### 8.2 Components

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

### 8.3 Resources

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

### 8.4 Deployers

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

### 8.5 RGD Instances (Configuration)

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

---

## 9. Upgrade Scenario

### 9.0 Upgrade Flow Overview

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

### 9.1 Version Bump

```yaml
# settings.yaml (v1.1.0)
VERSION: 1.1.0
POSTGRES_VERSION: "16-alpine"  # unchanged
DB_PASSWORD: "changeme-in-production"
```

With code changes:
```go
// Add a new endpoint in sovereign-notes
http.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(map[string]string{"version": "1.1.0"})
})
```

### 9.2 Release New Version

```bash
# Build, sign, push v1.1.0
VERSION=1.1.0 task release

# Transfer to air-gap
VERSION=1.1.0 task transfer:airgap

# Load into local registry
task cluster:load
```

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

---

## 10. Repository Layout

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

```bash
# Run unit tests (fast, no cluster needed)
task test

# Run integration tests (requires Docker + kind)
task test/integration

# Run full E2E (builds, signs, transfers, deploys, verifies)
task demo
```

---

## 12. Open Resource Discovery (ORD) Integration

[Open Resource Discovery (ORD)](https://open-resource-discovery.org/) is a Linux Foundation Europe protocol that enables applications to self-describe their exposed resources and capabilities. This integration demonstrates how sovereign-notes exposes its APIs via ORD for discovery by catalogs and marketplaces.

**Reference Implementation:** [ORD Reference Application](https://ord-reference-application.cfapps.sap.hana.ondemand.com/)

### 12.1 Architecture Overview

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

### 12.2 ORD Configuration Endpoint

The sovereign-notes service exposes an ORD configuration at the well-known endpoint:

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

### 12.3 ORD Document: Describing sovereign-notes

The ORD document describes the service's APIs, events, and metadata:

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

### 12.4 OCM Component with ORD Resource

The ORD document is bundled as a resource in the OCM component:

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

### 12.5 ORD Aggregator Integration

An ORD aggregator collects metadata from multiple providers:

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

### 12.6 Deployment with ORD Metadata

The OCM controller can expose ORD metadata from deployed components:

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

### 12.7 sovereign-notes ORD Implementation

Add ORD endpoints to the sovereign-notes application:

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

### 12.8 OpenAPI Specification

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

## 13. Deployment Extensibility

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

This design uses **kro ResourceGraphDefinitions** for deployment orchestration, enabling flexible target systems:

| Target            | How it integrates                                                       |
|-------------------|-------------------------------------------------------------------------|
| **FluxCD**        | RGD templates create `OCIRepository` + `HelmRelease` or `Kustomization` |
| **ArgoCD**        | RGD templates create `Application` CRs                                  |

The **component structure remains unchanged** — only the RGD templates vary per deployment target.

---

## 14. Key Design Decisions

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
