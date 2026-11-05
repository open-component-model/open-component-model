---
title: "Run the OCM Controllers in Production"
description: "Size, tune, and operate the OCM Kubernetes Controller Toolkit for production workloads."
weight: 80
toc: true
---

## Goal

The purpose of the guide is to provide a production grade configuration for the OCM Controller Toolkit.
Production grade configuration can be defined as configuration that lets the controller operate without
too much strain on a reasonable amount of objects without crashing or eating the cluster's resources.

## You'll end up with

- A controller deployment sized for the number of objects you run
- Concurrency and rate limits that match your cluster and registries
- High availability, metrics, and logging configured

**Estimated time:** ~10 minutes

## Prerequisites

- [Controller environment set up]({{< relref "docs/getting-started/setup-controller-environment.md" >}})
- [Concept: OCM controllers]({{< relref "docs/concepts/ocm-controllers.md" >}})

## Size memory and CPU

Memory scales approximately with the number of OCM objects the controller reconciles. The following numbers have been
tested in a restricted environment with ARM CPU architecture and in some runs, utilizing [GOMEMLIMIT](https://pkg.go.dev/runtime#pkg-overview).
Memory limit was applied via constraints on the deployment.

| Pipelines (complex) | Memory limit | `GOMEMLIMIT` | Peak working set |
|---------------------|--------------|--------------|------------------|
| up to ~1500         | `512Mi`      | not needed   | 273 MiB at 1000  |
| up to ~3000         | `1Gi`        | not needed   | 798 MiB at 3000  |
| up to ~4000         | `1Gi`        | `900MiB`     | 945 MiB at 4000  |

At 4000 pipelines the peak working set sits within 80 MiB of the `1Gi` limit. Treat that row as the ceiling, not a target,
and raise the limit if you expect to grow.

You might wonder, what is a `pipeline`? A pipeline is a complex component version with a number of resources, component references
and other data. It looks something like this:

```yaml
meta:
  schemaVersion: v2
component:
  name: ocm.software/perf/<run>/c0001
  version: 1.0.0
  provider:
    name: ocm.software
  resources:                         # 100 of these, image-0000 ... image-0099
    - name: image-0000
      version: 1.0.0
      type: ociImage
      relation: external
      access:
        type: OCIImage/v1
        imageReference: http://ocm-perf-registry:5000/perf/<run>/image-01:1.0.0   # pool[(i+j) % 10]
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: ociArtifactDigest/v1
        value: <manifest digest>
    # ...
  componentReferences:
    - name: child
      version: 1.0.0
      componentName: ocm.software/perf/<run>/c0001/n01
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: jsonNormalisation/v4alpha1
        value: <n01 digest>
signatures:
  - name: perf
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: jsonNormalisation/v4alpha1
      value: <digest>
    signature:
      algorithm: RSASSA-PSS
      value: <...>
```

In the cluster, 1000 pipelines mean 3000 CRs, up to 10 repositories and 10,000 component versions by default. This is set by
`--versions=5 --depth=5 --repositories=10` flags on the performance test run (not counting support objects like secrets,
config maps, service accounts, etc).

It's also signed and verified during the testing.

An approximate rule of thumb is that about 260 MiB is needed / 1000 _pipelines_. Here is a handy table using the default
settings:

| Object                                    | Per pipeline          | 1000 pipelines |
|-------------------------------------------|-----------------------|----------------|
| `Component` CRs                           | 1                     | 1000           |
| `Resource` and `NamespacedDeployer` CRs   | 2                     | 2000           |
| OCM components (names)                    | 6 (root + 5 nested)   | 6000           |
| Component versions                        | 10 (5 root + 5 chain) | 10,000         |
| Image resources per root version          | 100                   | 100 each       |
| Image resource entries, all root versions | 500                   | 500,000        |
| Distinct images in the registry           | 10 (shared pool)      | 10             |

CPU was pinned at the 500m default limit in every run. To increase speed, increase the CPU, but as of this writing, we
don't have precise correlation of data.

```yaml
manager:
  resources:
    limits:
      cpu: 500m # Though, we recommend not setting CPU limits as per https://medium.com/directeam/kubernetes-resources-under-the-hood-part-3-6ee7d6015965.
      memory: 2Gi
    requests:
      cpu: 100m
      memory: 1Gi
  env:
    - name: GOMEMLIMIT
      value: 1640MiB
```

### Set GOMEMLIMIT

GOMEMLIMIT, as described in the Go documentation can be useful to make the GC more aggressive towards memory restricted
environments. Unlike the Kubernetes environment's hard memory limit (going above gets you OOMKilled), this is a soft
memory limit. The Go environment will do its best to keep the memory below this number, giving your controller precious
seconds to catch up with work and evict unused go routines.

Best practice is to set GOMEMLIMIT to `~80%` of the limit.

## Tune concurrency and rate limits

The current defaults for concurrency and burst are as follows:

```yaml
manager:
  concurrency:
    repository: 4
    deployer: 4
    component: 4
    resource: 4
    replication: 4
    discovery: 4
  rateLimiter:
    # ... a lot more settings here...
    resource:
      qps: 10
      burst: 100
    # ...
```

This has been used during the establishment of the above numbers. Previous runs that were running with `concurrency: 1`
were less effective. The precise number depends on many factors, but using `concurrency: 1` resulted in at least 35%
reconciliation speed drop, but caused less memory pressure. Therefore, if you would like to alleviate memory pain,
and care less about reconciliation time, lower the concurrency settings.

`qps` and `burst` defaults were used during testing the above, and they were enough to drain 4000 pipelines. However, if
you experience troubles with requeuing time and handling time of the objects, increase these numbers as you see fit.

## Enable high availability

Leader election is on by default. On how to fine-tune leader election and high-availability, we recommend reading the
[Kubernetes - Leader Election docs](https://kubernetes.io/docs/concepts/architecture/leases/#leader-election).

Controller specific behavior is as follows:

- The helm chart enforces leader election in case of `replicas > 1`.
- Replicas are not reconciling. Only the leader is.
- At the moment of this writing, the chart template doesn't have `PodDisruptionBudget` values.

## Set up observability

### Metrics

Metrics are off by default. To enable them, set a bind address for the metrics server:

```yaml
manager:
  metricsServer:
    bindAddress: ":8443"
    secure: true
```

With `secure: true` the endpoint is served over TLS with a self-signed certificate. However, as of this writing, the
endpoint does no authentication or authorization, so metrics endpoint is open. Therefore, we recommend enabling
`manager.networkPolicy.enabled`, which admits the metrics port automatically, and restricting who can reach it.

The chart can create a Prometheus Operator `ServiceMonitor` with `prometheus.enable: true`. As of this writing, the chart
doesn't ship the metrics `Service` the `ServiceMonitor` selects, so you have to create it yourself:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: ocm-k8s-toolkit-controller-manager-metrics-service
  namespace: <release namespace>
  labels:
    app.kubernetes.io/name: ocm-k8s-toolkit
    control-plane: controller-manager
spec:
  selector:
    app.kubernetes.io/name: ocm-k8s-toolkit
    control-plane: controller-manager
  ports:
    - name: https
      port: 8443
      targetPort: 8443
```

Useful metrics to watch:

- `controller_runtime_reconcile_total`, `controller_runtime_reconcile_errors_total` and
  `controller_runtime_reconcile_time_seconds` per controller, to see how fast and how successfully objects reconcile.
- `workqueue_depth` and `workqueue_queue_duration_seconds`, to see whether the controller keeps up. Note that items
  waiting on the rate limiter are not counted in `workqueue_depth`, so an empty queue doesn't mean the controller is done.
- `process_resident_memory_bytes` and `go_memstats_heap_inuse_bytes`, to compare against your memory limit and
  `GOMEMLIMIT`.
- `ocm_system_ocm_k8s_toolkit_resolution_duration_seconds`, the time it takes to resolve component versions.

{{< callout context="caution" >}}
The resolution histogram is labelled per component and version, and the series are never removed. On a long-running
controller with many versions, the `/metrics` response and the memory it uses keep growing.
{{< /callout >}}

<!-- TODO: FIX THE METRICS LEAK!. -->

### Logging

Keep the defaults, `level: info` and `encoder: json`, so log aggregators can parse the output.

For troubleshooting, raise `manager.logging.level` to `debug`, or to `2` for the most verbose output.
The level is a container argument, so changing it restarts the pod!
Don't forget to revert it! Verbose, logs thousands of objects and increases log volume and CPU.

## Verify the setup

There is no load test you can safely run against a production cluster. Instead, check that the configuration
the controller is actually using, then watch keep an eye on the metrics as you start up your workflows.

1. Check the resources and the `GOMEMLIMIT` environment variable:

   ```bash
   kubectl -n <release namespace> get deploy -l control-plane=controller-manager \
     -o jsonpath='{.items[0].spec.template.spec.containers[0].resources}{"\n"}{.items[0].spec.template.spec.containers[0].env}{"\n"}'
   ```

2. Check that leader election works. The holder must be a running controller pod and the renew time must be recent:

   ```bash
   kubectl -n <release namespace> get lease 56490b8c.ocm.software \
     -o jsonpath='{.spec.holderIdentity}{"\n"}{.spec.renewTime}{"\n"}'
   ```

3. Check that the metrics endpoint serves controller metrics:

   ```bash
   kubectl -n <release namespace> port-forward svc/ocm-k8s-toolkit-controller-manager-metrics-service 8443 &
   curl -sk https://localhost:8443/metrics | grep controller_runtime_reconcile_total
   ```

4. Under real load, compare the working set against the memory limit and `GOMEMLIMIT`, and check that
   `workqueue_depth` drains:

   ```bash
   kubectl -n <release namespace> top pod -l control-plane=controller-manager
   ```

{{< callout context="tip" title="Rehearse before going live" >}}
To rehearse your sizing, run the `complex` scenario of the
[performance suite](https://github.com/open-component-model/open-component-model/tree/main/bindings/go/kubernetes/controller/test/performance)
on a throwaway Kind cluster. From `bindings/go/kubernetes/controller/` in a checkout of the repository:

```bash
# assumes NOT running in production because the performance suite uses http metrics
task test/performance/setup/local
helm upgrade ocm-k8s-toolkit chart/ --kube-context kind-ocm-perf -n ocm-k8s-toolkit-system \
  --reuse-values -f <your production values> --wait
go run ./test/performance --scenarios=complex --objects=<your object count> --repeats=1
task test/performance/setup/teardown
```

Use `--versions` and `--depth` to match your component graph.
{{< /callout >}}

## Related documentation

- [Configure Controller Credentials]({{< relref "docs/guides/deploy/configure-controller-credentials.md" >}})
- [Configure Custom RBAC]({{< relref "docs/guides/deploy/configure-custom-rbac.md" >}})
- [Verify Component Signatures on Deployment]({{< relref "docs/guides/deploy/verify-component-signatures-on-deployment.md" >}})
