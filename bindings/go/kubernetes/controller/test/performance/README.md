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
task test/performance                  # installs the chart, then the full matrix: 8 scenarios x 100/500/1000 objects x 3 repeats
task test/performance -- --scenarios=cold --objects=100 --repeats=1   # a subset
task test/performance/setup/teardown
task test/performance/fresh            # teardown, setup and run in one go
```

The flow mirrors the e2e tasks (`test/e2e/setup/local`, `test/e2e`, `test/e2e/setup/teardown`, `test/e2e/fresh`).

On a laptop, `cold` and `pipeline` at 1,000 objects take about 1.5 and 3 minutes per run, plus restart and cleanup. `go run ./test/performance --help` lists every flag.

## Environment

| Part | Setup |
|---|---|
| Cluster | Kind `ocm-perf`, one control-plane node plus one worker that is tainted for the controller only. Node image pinned by `KIND_NODE_IMAGE_VERSION` in `../../.env`. |
| Registry | `registry` container `ocm-perf-registry` outside Kind, pinned by `REGISTRY_IMAGE_VERSION` in `../../.env`. Host: `localhost:5555`, cluster: `ocm-perf-registry:5000`, metrics: `localhost:5556/metrics`. |
| Controller | The chart with [`hacks/values.yaml`](hacks/values.yaml): one replica, no leader election, Guaranteed QoS (2 CPU, 2Gi), and every concurrency and resolver knob set explicitly. |

The controller restarts before every run, so each run starts with empty in-memory caches. Every run publishes
fresh component names to the registry, so no run sees another run's component versions.

Only the Resource controller has a concurrency flag today. The Component and Deployer controllers run with
controller-runtime's default of one worker.

## Scenarios

`N` is the `--objects` value.

| Scenario | Objects created | Measured until |
|---|---|---|
| `cold` | 1 Repository, N Components with N unique component versions | every Component is `Ready` for its current generation |
| `shared` | 1 Repository, N Components that all resolve the same component version | every Component is `Ready` |
| `pipeline` | 1 Repository, N each of Component, Resource and Deployer | every Deployer is `Ready` |
| `update` | the `pipeline` objects, already rolled out at `1.0.0` (not measured) | every deployed ConfigMap carries `1.1.0` after all Components are patched to `1.1.0` |
| `versions` | 1 Repository, N Components with the constraint `>=1.0.0` on one component that has `--versions` versions (default 100) | every Component is `Ready` |
| `nested` | 1 Repository, N Components, N Resources that each walk a `--depth` long reference path (default 5) | every Resource is `Ready` |
| `repositories` | N Repositories, each a different `subPath` of the one registry, and N Components | every Component is `Ready` |
| `complex` | 10 Repositories on different `subPath`s, N each of Component, Resource and Deployer | every Deployer is `Ready` |

What the last four add over the first four:

- `versions`: an exact version skips the version listing, a constraint does not.
- `nested`: every hop of a reference path is one more component version to resolve. Each object has its own
  chain of `--depth` referencing components and one leaf that holds the resource.
- `repositories`: the controller caches repositories per repository spec, so N sub-paths give N cache entries
  and N resolvers without measuring more than one registry.
- `complex`: all of the above in one pipeline, shaped like a product delivery. Every object has a root
  component with 5 versions, selected by constraint, whose descriptor lists `--resources` OCI images
  (default 100) from a small pool pushed to the registry. The root reaches the deployed manifest through a
  `--depth` long reference path.

The Deployers apply one ConfigMap each. The ConfigMap's data holds the component version, so the update scenario
reads the rollout from the cluster itself, not from a status field.

Objects are created in parallel, the Repository first, the way a GitOps tool applies a directory. A watch
records when each object first becomes ready.

## Results

Each invocation writes `test/performance/results/<timestamp>/`, which is git-ignored:

- `summary.md`: one row per run. The Notes column flags runs that timed out or lost the controller; the
  benchmark exits non-zero when any run is flagged.
- `<run>/result.json`: the full result. It records environment (image digest, controller args and
  resources, Kubernetes version), durations (total, first ready, p50/p90/p99, publish, create),
  outcome (ready, failed, pending, failure reasons), usage, counter deltas and sampled gauge peaks.
- `<run>/controller-{start,end}.prom`, `<run>/registry-{start,end}.prom`: raw scrapes.
- `<run>/samples.json`: gauges and memory over time, `at` in seconds since the measurement started.

Metric sources:

| Measurement | Source |
|---|---|
| CPU seconds | delta of the controller's `process_cpu_seconds_total` |
| Peak RSS | max of the controller's `process_resident_memory_bytes`, sampled every `--sample-interval` |
| Peak working set | max of kubelet `container_memory_working_set_bytes`. cAdvisor refreshes it on its own housekeeping interval, not per scrape, so it lags on short runs. |
| OCI requests | delta of the registry's `registry_http_requests_total`, split by handler and method |
| Controller metrics | deltas of every `controller_runtime_*`, `workqueue_*`, `ocm_system_*` and `rest_client_requests_total` series. Per-component labels are folded away. |

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
