# ocm-k8s-toolkit

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for deploying the OCM Kubernetes Toolkit controller

**Homepage:** <https://ocm.software>

## Description

The OCM Kubernetes Toolkit controller manages OCM (Open Component Model) resources in Kubernetes clusters.
It provides controllers for:
- **Repository** - OCM repository references
- **Component** - OCM component version tracking
- **Resource** - OCM resource extraction
- **Deployer** - Resource deployment automation

## Installation

```bash
helm install ocm-k8s-toolkit oci://ghcr.io/open-component-model/charts/ocm-k8s-toolkit \
  --namespace ocm-system \
  --create-namespace
```

## Upgrading

```bash
helm upgrade ocm-k8s-toolkit oci://ghcr.io/open-component-model/charts/ocm-k8s-toolkit \
  --namespace ocm-system
```

## Uninstallation

```bash
helm uninstall ocm-k8s-toolkit --namespace ocm-system
```

> **Note:** CRDs are kept by default when uninstalling. To remove them:
> ```bash
> kubectl delete crd components.delivery.ocm.software deployers.delivery.ocm.software repositories.delivery.ocm.software resources.delivery.ocm.software
> ```

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| OCM Team |  | <https://github.com/open-component-model> |

## Source Code

* <https://github.com/open-component-model/open-component-model>

## Requirements

Kubernetes: `>=1.26.0-0`

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| certManager.enable | bool | `false` | Enable cert-manager for TLS certificates |
| crd.enable | bool | `true` | Install CRDs with the chart |
| crd.keep | bool | `true` | Keep CRDs when uninstalling |
| manager.affinity | object | `{}` | Pod affinity rules |
| manager.cache.deployerDownloadSize | int | `1000` | Maximum size of the deployer download object LRU cache |
| manager.cache.ocmContextSize | int | `100` | Maximum number of active OCM contexts kept alive |
| manager.cache.ocmSessionSize | int | `100` | Maximum number of active OCM sessions kept alive |
| manager.concurrency.resource | int | `4` | Number of active resource controller workers |
| manager.env | list | `[]` | Environment variables for the controller |
| manager.events.address | string | `""` | Address of the events receiver (optional) |
| manager.extraArgs | list | `[]` | Extra arguments to pass to the controller |
| manager.healthProbe.bindAddress | string | `":8081"` | Address the health probe endpoint binds to |
| manager.image.pullPolicy | string | `"IfNotPresent"` | Image pull policy |
| manager.image.repository | string | `"ghcr.io/open-component-model/kubernetes/controller"` | Controller image repository |
| manager.image.tag | string | `"1.2.3"` | Controller image tag |
| manager.imagePullSecrets | list | `[]` | Image pull secrets for the controller |
| manager.leaderElection.enabled | bool | `true` | Enable leader election for controller manager |
| manager.livenessProbe.initialDelaySeconds | int | `15` | Initial delay before starting liveness probes |
| manager.livenessProbe.path | string | `"/healthz"` | Path for the liveness probe |
| manager.livenessProbe.periodSeconds | int | `20` | Period between liveness probes |
| manager.livenessProbe.port | int | `8081` | Port for the liveness probe |
| manager.logging.development | bool | `false` | Enable development mode (console encoder, debug level, warn stacktrace) |
| manager.logging.encoder | string | `"json"` | Log encoding: 'json' or 'console' |
| manager.logging.level | string | `"info"` | Zap log level: 'debug', 'info', 'error', 'panic' or integer > 0 |
| manager.metricsServer.bindAddress | string | `"0"` | Address the metric endpoint binds to. Set to "0" to disable |
| manager.metricsServer.enableHttp2 | bool | `false` | Enable HTTP/2 for metrics and webhook servers |
| manager.metricsServer.secure | bool | `false` | Serve metrics endpoint securely |
| manager.nodeSelector | object | `{}` | Node selector for pod scheduling |
| manager.podSecurityContext | object | `{"runAsNonRoot":true}` | Pod-level security context |
| manager.readinessProbe.initialDelaySeconds | int | `5` | Initial delay before starting readiness probes |
| manager.readinessProbe.path | string | `"/readyz"` | Path for the readiness probe |
| manager.readinessProbe.periodSeconds | int | `10` | Period between readiness probes |
| manager.readinessProbe.port | int | `8081` | Port for the readiness probe |
| manager.replicas | int | `1` | Number of controller manager replicas |
| manager.resolver.workerCount | int | `10` | Number of active resolver workers |
| manager.resolver.workerQueueLength | int | `100` | Maximum work items in queue for component version resolution |
| manager.resources | object | `{"limits":{"cpu":"500m","memory":"128Mi"},"requests":{"cpu":"10m","memory":"64Mi"}}` | Resource limits and requests |
| manager.securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}}` | Container-level security context |
| manager.tolerations | list | `[]` | Pod tolerations |
| metrics.enable | bool | `false` | Enable metrics endpoint with RBAC protection |
| metrics.port | int | `8443` | Metrics server port |
| prometheus.enable | bool | `false` | Enable Prometheus ServiceMonitor (requires prometheus-operator) |
| rbacHelpers.enable | bool | `false` | Install convenience admin/editor/viewer roles for CRDs |

## Development

Run these tasks from the `kubernetes/controller` directory.

### Regenerating CRDs and RBAC

When API types or RBAC markers change, regenerate the Helm chart manifests:

```bash
task helm/sync-manifests
```

This runs `kubebuilder edit --plugins=helm/v2-alpha` which:
1. Runs `controller-gen` to generate CRDs and RBAC from Go source markers
2. Converts kustomize manifests to Helm-templated manifests in `chart/templates/`

> **Note:** Only CRDs and RBAC manifests are regenerated automatically. Other templates (e.g., `manager.yaml`, `_helpers.tpl`) are managed manually.

### Validating changes

Before committing, run validation to ensure all generated files are in sync:

```bash
task helm/validate
```

This checks:
- Chart linting passes
- Templates render successfully
- CRDs, RBAC, schema, and docs are up to date

### Regenerating artifacts after values.yaml changes

```bash
task helm/schema    # Regenerate values.schema.json
task helm/docs      # Regenerate README.md
```

### Packaging the chart

Package the chart for distribution:

```bash
task helm/package                                    # Use versions from Chart.yaml
task helm/package VERSION=1.0.0                      # Override chart version
task helm/package APP_VERSION=1.0.0                  # Override app version (image tag)
task helm/package VERSION=1.0.0 APP_VERSION=1.0.0   # Override both
```

The packaged chart is saved to `dist/ocm-k8s-toolkit-<version>.tgz`.

### Other useful tasks

```bash
task helm/template  # Render templates locally
task helm/install   # Install chart to current cluster
task helm/uninstall # Remove chart from cluster
```
