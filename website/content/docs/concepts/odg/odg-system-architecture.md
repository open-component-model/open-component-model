---
title: "ODG System Architecture"
description: "Architectural overview of ODG's core components, extension mechanisms, and scheduling."
weight: 1
toc: true
---

Open Delivery Gear (ODG) is a Kubernetes-native compliance automation engine designed for continuous security and compliance scanning of software components modelled with the Open Component Model (OCM). This document provides an architectural overview of ODG's core components, extension mechanisms, scheduling and persistence architecture, and common entry points into the system.

## Overview

```mermaid
flowchart TD
    core[ODG Core] --> db(ODG-DB)
    db --> core
    extensions_cfg(Extension Configuration) --> artefact_enumerator[Artefact Enumerator]
    secrets(Credentials) --> artefact_enumerator
    artefact_enumerator -->|Create| backlog_items(Backlog Items)
    cronjob[Cronjob] -->|trigger| artefact_enumerator
    backlog_items --> backlog_controller[Backlog Controller]
    backlog_controller -->|scale| scanner[Scanner]
    extensions_cfg --> scanner
    findings_cfg(Finding Configuration) --> scanner
    secrets --> scanner
    scanner -->|Claim| backlog_items
    scanner -->|Upload Result| core
    scanner -->|Delete| backlog_items
    core --> ui[ODG UI]
```

## Design Principles

ODG is built upon several key architectural principles:

- **Kubernetes-Native Deployment**: All components are designed as Kubernetes workloads with native resource management
- **Modular and Extensible**: Core functionality is minimal; features are added through extensions
- **Loose Coupling**: Components communicate through well-defined APIs and can be scaled or replaced independently
- **Asynchronous Processing**: Work is distributed through a queue-based system enabling autonomous operation
- **Single Source of Truth**: All persistent state resides in the ODG database, ensuring consistency

## Core Components

The ODG system comprises several core components that provide the foundation for all operations.

### ODG Database (odg-db)

The ODG Database serves as the **sole persistency layer** for the entire system. All findings, metadata, compliance snapshots, and configuration state flow through this component.

**Responsibilities:**

- Store findings and metadata as `ArtefactMetadata` entries
- Correlate metadata with OCM coordinates (component name, version, artefact identity)
- Maintain compliance snapshots tracking artefact processing state
- Store scanner metadata writebacks and rescoring decisions
- Track discovery dates for SLA enforcement
- Cache OCM component information

**Architecture:**
The database uses a correlation model where all data is linked to an `artefact` identifier. This allows grouping findings and metadata across different scans, versions, and extensions whilst maintaining traceability to specific OCM components or runtime artefacts.

### ODG Core (odg-core)

ODG Core provides the **central API** layer and serves as the primary entry point for both human users and automated systems.

**Responsibilities:**

- Expose API endpoints for CRUD operations on artefact metadata
- Proxy database operations with business logic enforcement
- Handle authentication and authorisation (implemented via OAuth)
- Serve data payloads for the ODG UI application
- Provide high-level OCM functions, e.g.:
  - List component versions
  - Calculate differences between OCM component versions
  - Resolve component dependencies recursively
- Maintain artefact scan information
- Coordinate metadata queries with complex filtering

The core API is the single gateway through which extensions upload findings, the UI retrieves data, and external systems integrate with ODG.

### ODG UI (odg-ui)

The Delivery Dashboard provides the **primary user interface** for interacting with ODG.

**Capabilities:**

- Browse OCM components and their artefacts
- View compliance status and findings with SLA tracking
- Create and manage finding assessments and rescorings
- Submit scanner metadata writebacks
- Monitor ODG system health (pod status, backlog queue depth)

The UI is a static web application served by a dedicated webserver, consuming data exclusively through the ODG Core API.

### Artefact Enumerator

The Artefact Enumerator acts as the **orchestration engine** for automated scanning workflows.

**Responsibilities:**

- Periodically check configured OCM components for new versions
- Resolve OCM component dependencies recursively
- Manage compliance snapshot lifecycle (create, update, delete)
- Evaluate which extensions need to process which artefacts
- Create `BacklogItem` custom resources to trigger extension processing
- Track grace periods for artefacts no longer of interest

**How It Works:**

The artefact enumerator runs as a Kubernetes CronJob on a configurable schedule. For each configured OCM component:

1. **Discovery**: Fetch the component descriptor and recursively resolve all dependencies
2. **Tracking**: Create or update compliance snapshots for each artefact (resource or source)
3. **Change Detection**: Compare current state against previous scan information
4. **Triggering**: For each extension, evaluate if scanning is needed based on:
   - New artefact versions
   - Configured scan interval elapsed
   - Explicit manual trigger
5. **Backlog Creation**: Generate `BacklogItem` custom resources for required scans

Compliance snapshots persist state across enumerator runs, ensuring stable tracking of what needs processing and enabling graceful cleanup when artefacts are removed from the configuration.

### Backlog Items

Backlog Items are **Kubernetes Custom Resources** (CRs) that represent queued work for extensions.

**Structure:**
```yaml
apiVersion: delivery-gear.gardener.cloud/v1
kind: BacklogItem
metadata:
  name: <extension>-<hash>
  namespace: <my-namespace>
  labels:
    delivery-gear.gardener.cloud/service: <extensionName>
    delivery-gear.gardener.cloud/claimed: "<Boolean>"
  annotations:
    delivery-gear.gardener.cloud/claimed-at: <timestamp>
    delivery-gear.gardener.cloud/claimed-by: <extensionName>-<suffix>
spec:
  artefact:
    component_name: example.org/component
    component_version: 1.0.0
    artefact_kind: resource
    artefact:
      artefact_name: my-image
      artefact_version: 1.0.0
      artefact_type: ociImage
      artefact_extra_id: {}
  priority: 8
  timestamp: '2025-01-01T12:00:00.000000+00:00'
```

**Properties:**

- **Priority**: Determines processing order (higher number = higher priority)
- **Service Label**: Associates the item with a specific extension
- **Artefact Identity**: Fully qualified OCM or runtime artefact reference
- **Timestamp**: Creation time for audit and staleness detection

Backlog Items implement a **priority queue** pattern. Extensions claim items by adding a claim annotation, process the artefact, then delete the item upon completion.

### Backlog Controller

The Backlog Controller provides **dynamic scaling** of extension workers based on queue depth.

**Responsibilities:**

- Watch `BacklogItem` custom resources across all extensions
- Calculate required replica count per extension based on pending items
- Scale Kubernetes Deployments up or down to match workload
- Detect and release stale claims (items claimed but not processed within timeout)

The controller ensures efficient resource utilisation by scaling workers up during high load and down during idle periods, whilst preventing runaway scaling and resource exhaustion.

## Extensions

Extensions are **modular components** that implement specific scanning, analysis, or reporting capabilities. They operate independently and communicate only through the ODG Core API.

### Extension Types

ODG supports two integration models:

1. **Fully Integrated (In-Cluster)**
   - Deployed as part of the ODG Helm chart
   - Scaled automatically by the backlog controller
   - Consume configuration from ConfigMaps
   - Trigger via backlog items or CronJob schedule

2. **Lightly Integrated (Out-of-Cluster)**
   - Run externally (CI pipeline, separate cluster, local machine)
   - Upload findings via ODG Core API
   - Manage their own deployment and triggering
   - Benefit from ODG's reporting and SLA tracking

### Scanner Extensions

Scanners are the most common extension type, performing security or compliance analysis on artefacts.

**Lifecycle:**

1. **Claim**: Worker queries for pending backlog items and claims one
2. **Fetch**: Retrieve artefact content (OCI image, source code, etc.)
3. **Scan**: Execute analysis (vulnerability detection, malware scanning, licence checks)
4. **Upload**: Submit findings as `ArtefactMetadata` to ODG Core API
5. **Cleanup**: Delete obsolete findings and mark scan complete
6. **Delete**: Remove the backlog item to signal completion

**Examples:**

- **Vulnerability Scanner (BDBA)**: Detects known vulnerabilities in software packages
- **Malware Scanner (ClamAV)**: Scans artefacts for malicious content
- **Cryptographic Asset Inventory**: Catalogues cryptographic material
- **OS End-of-Life Detection**: Identifies unsupported operating system versions
- **SBoM Generator**: Creates Software Bill of Materials

### Extension Triggers

Extensions can be triggered in two ways:

1. **Backlog-Driven** (Recommended)
   - Extension deployed as Kubernetes Deployment
   - Scaled automatically by backlog controller
   - Workers run a continuous loop claiming and processing items
   - Triggered by artefact enumerator or manual backlog item creation

2. **Schedule-Driven**
   - Extension deployed as Kubernetes CronJob
   - Runs at fixed intervals regardless of artefact changes
   - Must determine target artefacts from configuration
   - Suitable for periodic reporting or aggregation tasks

### Extension Data Model

Extensions communicate through the `ArtefactMetadata` model:

**Structure:**

- **artefact**: OCM or runtime artefact identity (correlation ID)
- **meta**: Datasource, type, discovery date, processing time allowance
- **data**: Extension-specific payload (findings, informational data)

**Metadata Types:**

1. **Meta Types**: System-level tracking (e.g., `meta/artefact_scan_info`)
2. **Finding Types**: Deviations requiring remediation (e.g., `finding/vulnerability`)
3. **Informational Types**: Enrichment data (e.g., file paths, package inventories)

Each metadata entry has a unique **key** derived from artefact identity, datasource, type, and payload key, enabling idempotent updates and discovery date retention.

### Common Extensions

Beyond scanners, ODG includes several specialised extensions:

**Issue Replicator**

- Creates and manages GitHub issues for findings
- Groups findings by artefact, type, and due date
- Assigns issues to responsible teams
- Closes issues when findings are remediated or artefacts removed

**Responsibles Extension**

- Determines ownership for artefacts based on configurable rules
- Uploads responsible information for use by issue replicator
- Supports multiple strategies (static, component-based, CODEOWNERS)

## Instance-Specific Configuration

ODG instances are customised through Kubernetes-native configuration resources.

### Extension Configuration

Deployed as a **ConfigMap** (`extensions-cfg`), this configuration defines:

- Which extensions are enabled
- Extension-specific parameters (API endpoints, intervals, filters)
- OCM components to track
- Backlog controller scaling parameters

**Example:**

```yaml
artefact_enumerator:
  components:
    - component_name: example.org/my-component
      ocm_repo_url: europe-docker.pkg.dev/gardener-project/releases
      version: greatest
      max_versions_limit: 1
```

### Finding Configuration

Deployed as a **ConfigMap** (`findings-cfg`), this configuration defines:

- Supported finding types
- Severity categorisations
- Allowed processing times (SLAs) per severity
- GitHub issue reporting configuration
- Artefact grouping rules

**Example:**

```yaml
- type: finding/vulnerability
  categorisations:
    - id: CRITICAL
      display_name: Critical
      allowed_processing_time: 30
      rescoring: manual
      selector:
        cve_score_range:
          max: 10
          min: 9
      value: 8
  issues:
    enable_issues: true
    attrs_to_group_by:
      - component_name
      - artefact.artefact_name
```

### Credentials

Sensitive information (API keys, GitHub tokens, registry credentials) is stored as **Kubernetes Secrets** and mounted into relevant pods.

## Persistence Architecture

All persistent state resides in the **ODG Database**, accessed exclusively through the **ODG Core API**.

### Data Organisation

**Artefact Metadata**

- Indexed by artefact identity (component, version, artefact name/type)
- Supports efficient queries by component, type, datasource, or custom attributes
- Retains discovery dates across updates for consistent SLA tracking

**Compliance Snapshots**

- Track processing state per artefact
- Store last scan timestamps per extension
- Enable graceful cleanup of removed artefacts

**Scanner Metadata Writebacks**

- Persist corrections to scanner-detected metadata
- Scoped from single artefact to global across components
- Applied by extensions before reporting results

**Rescorings**

- Override finding severity or SLA deadlines
- Support temporary exceptions with expiration

**Caching**

- OCM component descriptors cached to reduce network overhead
- Responsible lookups cached to accelerate issue assignment

### Consistency and Durability

- **Single Writer**: Only ODG Core API writes to the database
- **Idempotent Updates**: Metadata updates use stable keys to prevent duplication
- **Discovery Date Retention**: Custom logic preserves initial discovery dates even when finding keys change
- **Backup**: Database backup extension provides scheduled exports

## Common Entry Points

### For End Users

**ODG UI**

- Browse components: Navigate OCM component hierarchy
- View findings: Filter by severity, due date, responsible team
- Create assessments: Rescorings, false positive markings
- Submit writebacks: Correct scanner-detected metadata
- Monitor system: View backlog depth, pod health

**ODG Core API**

- Programmatic access to all UI capabilities
- Integration with CI/CD pipelines
- Custom tooling and automation
- Available at `/api/v1/doc` for OpenAPI specification

### For Extensions

- See [Scanner Extensions](#scanner-extensions) for architecture overview
- See [Contribute a new Extension]({{< relref "docs/tutorials/odg/contribute-a-new-extension/" >}}) for development guide

### For Operators

**Kubernetes API**

- Create/update `BacklogItem` CRDs for manual triggering
- Create `RuntimeArtefact` CRDs for non-OCM artefacts
- Monitor pod status and resource utilisation
- Scale extensions via Deployment replicas (auto-scaled by backlog controller)

**Configuration Management**

- Update `extensions-cfg` ConfigMap to enable/disable extensions
- Update `findings-cfg` ConfigMap to adjust SLAs or issue settings
- Manage Secrets for credentials

## Summary

ODG's architecture achieves compliance automation through:

- **Separation of Concerns**: Core, extensions, UI, and database operate independently
- **Queue-Based Processing**: Asynchronous work distribution with automatic scaling
- **Single Persistence Layer**: All state in ODG Database ensures consistency
- **Extensibility**: New scanners integrate without core changes
- **Kubernetes-Native**: Leverages CRDs, scaling, and native resource management

This design enables ODG to continuously monitor software deliveries, track findings against SLAs, and automate remediation workflows whilst remaining adaptable to diverse security and compliance requirements.
