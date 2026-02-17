# OCM Conformance Scenario: Sovereign Cloud Delivery

This conformance scenario validates OCM's core value proposition: **modeling, signing, transporting, and deploying a multi-service product into an air-gapped sovereign cloud environment**.

## Overview

Implements the reference scenario defined in [ADR-0013](../../../docs/adr/0013_sovereign_cloud_reference_scenario.md) to demonstrate:

- Real multi-service application with genuine dependencies
- Component modeling and construction
- Digital signing and verification workflows
- Air-gap transport via CTF (Common Transport Format)
- Deployment on air-gapped infrastructure using OCM Controller + kro + FluxCD

## Architecture

The scenario deploys a minimal notes application with PostgreSQL backend:

- **sovereign-notes**: Go web service with REST API for notes management
- **PostgreSQL**: Official postgres image deployed as StatefulSet
- **sovereign-product**: Meta component that orchestrates both services

## Quick Start

```bash
# Prerequisites: kind, flux CLI, ocm CLI, task
# Install instructions: https://ocm.software/dev/docs/getting-started/install-the-ocm-cli/

# Run full conformance scenario
task demo

# Or run step-by-step
task build:ctf
task sign
task transfer:airgap
task cluster:create
task cluster:load
task cluster:deploy
task verify:deployment
```

## What This Validates

### Core OCM Capabilities

- ✅ Component construction from multiple input types (source code, helm charts, container images)
- ✅ Component dependency modeling via componentReferences  
- ✅ Resource bundling and self-contained transport archives
- ✅ RSA digital signing with RSASSA-PSS algorithm
- ✅ Signature verification during transfer and deployment
- ✅ Cross-registry transport with resource localization

### Air-Gap Deployment

- ✅ CTF-based transport without internet connectivity
- ✅ Local registry integration
- ✅ Image reference rewriting for air-gapped registries
- ✅ Configuration management via ResourceGraphDefinitions (kro)

### Ecosystem Integration

- ✅ OCM Controller for Kubernetes-native component management
- ✅ FluxCD for GitOps-based workload deployment  
- ✅ Helm chart deployment with dynamic values injection
- ✅ Kubernetes resource orchestration and health checking

## Directory Structure

```
sovereign-scenario/
├── README.md                    # This file
├── Taskfile.yml                # Build and deployment automation
├── settings.yaml               # Version configuration
├── keys/                       # Signing keys (public key committed)
├── components/                 # OCM component definitions
│   ├── notes/                 # Notes application component
│   ├── postgres/              # PostgreSQL component  
│   └── product/               # Meta component
├── deploy/                    # OCM controller deployment manifests
├── scripts/                   # Setup and utility scripts
└── tests/                     # Test suites
    ├── integration/          # Integration tests
    ├── conformance/          # Conformance validation
    └── e2e/                  # End-to-end tests
```

## Success Criteria

A successful conformance run validates:

1. **Component Construction**: All three components build without errors
2. **Signing**: Components are signed with RSA-PSS and signatures verify
3. **Transport**: CTF archive transfers to air-gapped registry with resource localization
4. **Deployment**: OCM Controller successfully deploys all components
5. **Integration**: Notes service connects to PostgreSQL and serves traffic
6. **Upgrade**: Version bump triggers automatic rolling update

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| `ocm` | >= 0.18.0 | OCM CLI for component operations |
| `kind` | >= 0.20.0 | Local Kubernetes cluster |
| `flux` | >= 2.2.0 | GitOps deployment |
| `task` | >= 3.0.0 | Build automation |
| `kubectl` | >= 1.28.0 | Kubernetes CLI |
| `docker` | >= 24.0.0 | Container runtime |

## Troubleshooting

### Common Issues

- **Build failures**: Check Docker is running and OCM CLI is properly installed
- **Registry connection**: Ensure kind cluster can reach localhost:5001 registry
- **Component verification**: Verify signing keys are properly configured
- **Deployment timeouts**: Check cluster resources and OCM controller logs

### Debug Commands

```bash
# Check component status
kubectl -n ocm-system get components

# View controller logs  
kubectl -n ocm-system logs -l app=ocm-controller

# Verify signature status
ocm verify cv ./transport-archive//acme.org/sovereign/product:1.0.0

# Test application connectivity
kubectl -n sovereign-product port-forward svc/notes 8080:80
curl http://localhost:8080/notes
```

## Integration Points

This scenario serves as a conformance test for:

- OCM CLI component operations
- OCM Controller Kubernetes integration
- CTF transport format compatibility  
- ResourceGraphDefinition (kro) deployment patterns
- FluxCD source controller integration
- Multi-component dependency resolution
- Air-gap registry workflows

## Contributing

To extend this scenario:

1. Modify components in `components/` directories
2. Update version in `settings.yaml`  
3. Add tests in appropriate `tests/` subdirectory
4. Document changes in this README
5. Verify full conformance run passes

This scenario should remain a complete, working example that new OCM adopters can use as a reference implementation.