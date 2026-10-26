# Controller Performance Benchmark

A reproducible benchmark for the OCM controllers. It creates many controller objects against a local registry,
waits until they are ready, and records duration, success, CPU, memory, OCI registry requests and controller
metrics.

The structure follows [fluxcd/flux-benchmark](https://github.com/fluxcd/flux-benchmark): a dedicated Kind
cluster, a local OCI registry, parameterized workloads, and install and update scenarios.

## Quick start

Requires `docker`, `kind`, `kubectl` and `helm`. All commands run from `bindings/go/kubernetes/controller/`.

```bash
task test/performance/setup/local      # Kind cluster + registry, builds and loads the controller from the working tree
task test/performance                  # installs the chart, then the full matrix: 5 scenarios x 100/500/1000 objects x 3 repeats
task test/performance -- --scenarios=cold --objects=100 --repeats=1   # a subset
task test/performance/setup/teardown
task test/performance/fresh            # teardown, setup and run in one go
```

The flow mirrors the e2e tasks (`test/e2e/setup/local`, `test/e2e`, `test/e2e/setup/teardown`, `test/e2e/fresh`).

`go run ./test/performance --help` lists every flag.

## Structure

The benchmark is a standalone Go program, run with `go run`. It builds and measures the working tree only.

| File                     | Role                                                                      |
|--------------------------|---------------------------------------------------------------------------|
| `main.go`                | Flags and the matrix loop: scenarios x object counts x repeats            |
| `workload.go`            | Per scenario: the component versions to publish and the objects to create |
| `publish.go`, `image.go` | Push component versions and a small image pool to the registry            |
| `signing.go`             | Per-run RSA key, signatures, and the controller's verification config     |
| `cluster.go`             | Kubernetes client, controller restart, metric scrapes, cleanup            |
| `tracker.go`             | A watch that records when each target object first becomes ready          |
| `sampler.go`             | Polls gauges, CPU and memory every `--sample-interval`                    |
| `metrics.go`             | Prometheus parsing, counter deltas, gauge values                          |
| `run.go`                 | One run from start to finish                                              |
| `result.go`              | `result.json` and `summary.md`                                            |
| `hacks/`                 | Cluster and registry setup, Helm values                                   |

One run restarts the controller, publishes fresh component names, scrapes the metrics, creates the objects,
waits until they are ready and the controller queues are drained, scrapes again and cleans up.

## Environment

| Part       | Setup                                                                                                                                                                                                                                                                                                                                                                          |
|------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Cluster    | Kind `ocm-perf`, one control-plane node plus one worker that is tainted for the controller only. Node image pinned by `KIND_NODE_IMAGE_VERSION` in `../../.env`.                                                                                                                                                                                                               |
| Registry   | `registry` container `ocm-perf-registry` outside Kind, pinned by `REGISTRY_IMAGE_VERSION` in `../../.env`. Host: `localhost:5555`, cluster: `ocm-perf-registry:5000`, metrics: `localhost:5556/metrics`.                                                                                                                                                                       |
| Controller | The chart with [`hacks/values.yaml`](hacks/values.yaml): one replica, no leader election, Guaranteed QoS (2 CPU, 2Gi), and every concurrency, rate limiter and resolver knob set explicitly. The workqueue rate limiter is raised to 10000/s: at the chart default of 10/s the requeue after adding a finalizer admits 10 new objects per second, which hides everything else. |

The controller restarts before every run, so each run starts with empty in-memory caches. Every run publishes
fresh component names to the registry, so no run sees another run's component versions.

Of the benchmarked controllers only the Resource controller has a concurrency flag. The Component and
NamespacedDeployer controllers run with controller-runtime's default of one worker.

## Scenarios

`N` is the `--objects` value.

| Scenario  | Objects created                                                                                                  | Measured until                                                                       |
|-----------|------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------|
| `cold`    | 1 Repository, N Components with N unique component versions                                                      | every Component is `Ready` for its current generation                                |
| `shared`  | 1 Repository, N Components that all resolve the same component version                                           | every Component is `Ready`                                                           |
| `update`  | 1 Repository, N each of Component, Resource and NamespacedDeployer, already rolled out at `1.0.0` (not measured) | every deployed ConfigMap carries `1.1.0` after all Components are patched to `1.1.0` |
| `complex` | up to `--repositories` Repositories (default 10), N each of Component, Resource and NamespacedDeployer, signed   | every NamespacedDeployer is `Ready`                                                  |
| `fanout`  | 1 Repository, N Components, `--fanout` Resources per Component (default 20)                                      | every Resource is `Ready`                                                            |

What each one isolates:

- `cold`: the baseline. Every Component misses the resolver cache once.
- `shared`: the opposite. One cache miss, then N - 1 hits on the same component version.
- `update`: re-resolving and redeploying a version change, not a first install.
- `complex`: a product delivery in one pipeline. Every object has a root component with `--versions`
  versions (default 5), selected by the constraint `>=1.0.0`, whose descriptor lists `--resources` OCI
  images (default 100) from a small pool pushed to the registry. The root reaches the deployed manifest
  through a `--depth` long reference path (default 5). The objects share up to `--repositories`
  Repositories, each a different `subPath` of the one registry, so the controller holds that many
  repository cache entries.
- `fanout`: load on the Resource controller, the only one of the three with more than one worker. Every
  Component has one component version whose descriptor lists `--resources` OCI images, and each of its
  Resources selects a different one. There are no Deployers, so the single Deployer worker does not bound
  the run. `Objects` in the results is N; the outcome counts all N x `--fanout` Resources.

`complex` replaces scenarios that each covered one of its knobs. To find which knob a `complex` regression
comes from, rerun it with that knob turned down: `--depth=0` puts the manifest into the root, `--versions=1`
skips choosing between versions, `--repositories=1` shares one Repository, `--sign=false` drops signing.

### Signing

`complex` signs every component version with RSASSA-PSS under the signature name `perf`, with a key
generated for the run. The Components reference a Secret that holds only the public key and a
`signing.config.ocm.software` entry for `perf`, with the `Propagate` policy, so the Resources and Deployers
verify the root as well. The controller verifies the root by signature and every component version below it
against the digest its parent records for it.

The controller accepts a descriptor that misses a resource or reference digest without verifying it at all.
The benchmark therefore writes every image and reference digest, and refuses to sign a component version
that is not safely digestible, the same check `ocm sign cv` makes. Component versions are published level
by level, leaves first, because a reference digest needs the referenced component version.

The NamespacedDeployers apply one ConfigMap each as the `perf-deployer` service account, which a Role in the run
namespace allows to write ConfigMaps. The ConfigMap's data holds the component version, so the update scenario
reads the rollout from the cluster itself, not from a status field.

Objects are created in parallel, the Repositories, the signing Secret and the deployer RBAC first, the way a GitOps tool
applies a directory. A watch records when each object first becomes ready.

The benchmark's own client is limited to 200 requests per second. A scenario that creates objects for longer
than the controller needs to reconcile them is bound by that limit, not by the controller.

## Results

Each invocation writes `test/performance/results/<timestamp>/`, which is git-ignored:

- `summary.md`: one row per run. The Notes column flags runs that timed out, did not drain or lost the controller; the
  benchmark exits non-zero when any run is flagged.
- `<run>/result.json`: the full result. It records environment (image digest, controller args and
  resources, Kubernetes version), durations (total, settled, first ready, p50/p90/p99, publish, create),
  outcome (ready, failed, pending, failure reasons), usage, counter deltas and sampled gauge peaks.
- `<run>/controller-{start,end}.prom`, `<run>/registry-{start,end}.prom`: raw scrapes.
- `<run>/samples.json`: gauges, cumulative CPU seconds and memory over time, `at` in seconds since the
  measurement started.

All durations count from the moment object creation starts, not from each object's own creation. `p50` and
`p90` therefore include the time the benchmark needed to create the objects that came before.

Metric sources:

| Measurement        | Source                                                                                                                                                                                 |
|--------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| CPU seconds        | delta of the controller's `process_cpu_seconds_total`, up to the settled time                                                                                                          |
| Avg cores          | CPU seconds divided by the settled time                                                                                                                                                |
| Peak cores         | highest rate of `process_cpu_seconds_total` between two consecutive samples. It is an average over one `--sample-interval`, so shorter bursts read lower.                              |
| Settled            | first of 3 consecutive samples with every `workqueue_depth` and `controller_runtime_active_workers` at zero, after all objects are ready. Counters and usage cover the run up to here. |
| Peak RSS           | max of the controller's `process_resident_memory_bytes`, sampled every `--sample-interval`                                                                                             |
| Peak working set   | max of kubelet `container_memory_working_set_bytes`. cAdvisor refreshes it on its own housekeeping interval, not per scrape, so it lags on short runs.                                 |
| OCI requests       | delta of the registry's `registry_http_requests_total`, split by handler and method                                                                                                    |
| Controller metrics | deltas of every `controller_runtime_*`, `workqueue_*`, `ocm_system_*` and `rest_client_requests_total` series. Per-component labels are folded away.                                   |

## Profiling

Swap in the pprof-enabled build of the working tree:

```bash
task docker-build/debug
kind load docker-image ghcr.io/open-component-model/kubernetes/controller:latest-debug --name ocm-perf
helm upgrade ocm-k8s-toolkit chart/ --kube-context kind-ocm-perf -n ocm-k8s-toolkit-system --reuse-values \
  --set manager.image.tag=latest-debug --wait
kubectl -n ocm-k8s-toolkit-system port-forward deploy/ocm-k8s-toolkit-controller-manager 6060 &
# The per-run restart would cut the port-forward, so keep the pod. `task test/performance` would
# reinstall the chart with the non-debug tag, so run the benchmark directly.
go run ./test/performance --scenarios=cold --objects=1000 --repeats=1 --restart-controller=false &
go tool pprof http://localhost:6060/debug/pprof/heap
```

Profiles cost CPU, so do not use numbers from a profiled run as benchmark results.
