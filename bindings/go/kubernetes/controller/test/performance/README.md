# Controller Performance Benchmark

A reproducible benchmark for the OCM controllers. It creates many controller objects against a local registry,
waits until they are ready, and records duration, success, CPU, memory, OCI registry requests and controller
metrics.

The structure follows [fluxcd/flux-benchmark](https://github.com/fluxcd/flux-benchmark): a dedicated Kind
cluster, a local OCI registry and a parameterized workload.

## Quick start

Requires `docker`, `kind`, `kubectl` and `helm`. All commands run from `bindings/go/kubernetes/controller/`.

```bash
task test/performance/setup/local      # Kind cluster + registry, builds and loads the controller from the working tree
task test/performance                  # installs the chart, then 100/500/1000 objects x 3 repeats
task test/performance -- --objects=100 --repeats=1   # a subset
task test/performance/setup/teardown
task test/performance/fresh            # teardown, setup and run in one go
```

The flow mirrors the e2e tasks (`test/e2e/setup/local`, `test/e2e`, `test/e2e/setup/teardown`, `test/e2e/fresh`).

`go run ./test/performance --help` lists every flag; see [Options](#options) for what they change.

### In CI

The `Controller: Performance Benchmark` workflow runs the same flow in four ways. Every run shows `summary.md` in
its job summary and uploads the results directory as an artifact.

| Trigger                   | Suite                                                                |
|---------------------------|----------------------------------------------------------------------|
| Nightly on `main`         | The standard suite: `--objects=1000 --repeats=3`                     |
| Weekly on `main` (Sunday) | Every object count: `--objects=100,500,1000 --repeats=3`             |
| Actions, Run workflow     | Any `--objects` and `--repeats`                                      |
| `/perf` comment on a PR   | The standard suite, for the PR and for its merge base                |

`/perf` takes no arguments, so every comparison runs the same suite. The workflow reacts with 👀 when it sees the
command, 🚀 when the runs are launched, and 😕 when the comment has arguments. When both runs succeed, it replies with a
table of the medians of both sides and the difference; when either fails, it replies with 🛑 and a link to the run.
Rows marked `*` depend on the runner, because the two sides run on different machines. A new `/perf` on the same pull
request cancels the runs of the previous one. Only repository owners, organization members and collaborators can
trigger it.

## Structure

The benchmark is a standalone Go program, run with `go run`. It builds and measures the working tree only.

| File                     | Role                                                                      |
|--------------------------|---------------------------------------------------------------------------|
| `main.go`                | Flags and the matrix loop: object counts x repeats                        |
| `workload.go`            | The workload: component names, versions and the manifests they deploy     |
| `objects.go`             | The controller objects, Repositories to Deployers, and their RBAC         |
| `publish.go`, `image.go` | Push component versions and a small image pool to the registry            |
| `signing.go`             | Per-run RSA key, signatures, and the controller's verification config     |
| `cluster.go`             | Kubernetes client, controller restart, metric scrapes, cleanup            |
| `phases.go`              | The phases: what each one prepares, changes and watches until it is done  |
| `tracker.go`             | Watcher: records when the object of each pipeline is first done           |
| `meter.go`               | Meter: scrapes at either end of a window, sampling, counter deltas, peaks |
| `sampler.go`             | Polls gauges, CPU and memory every `--sample-interval`                    |
| `metrics.go`             | Prometheus parsing, counter deltas, gauge values                          |
| `run.go`                 | One run from start to finish: setup, the phases in order, cleanup         |
| `measure.go`             | One phase: prepare, then watch and meter the window from its change on    |
| `result.go`              | `result.json` and `summary.md`                                            |
| `hacks/`                 | Cluster and registry setup, Helm values                                   |

One run restarts the controller and creates its namespace. It then measures its phases in order, each one the same
way: prepare (not measured), scrape the metrics, make the change, wait until the object of every pipeline shows it and
the controller queues are drained, and scrape again. A phase that times out, does not drain or loses the controller
ends the run: the next phase would not start from a known state. The run then deletes its namespace and waits until it
is gone.

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

## Workload

`N` is the `--objects` value. Every run creates, in one namespace, up to `--repositories` Repositories (default 10) and N
signed pipelines of a Component, a Resource and a NamespacedDeployer: a product delivery in one pipeline each.

Every object has a root component with `--versions` versions (default 5), selected by a version constraint, whose
descriptor lists `--resources` OCI images (default 100) from a small pool pushed to the registry. The root reaches the
deployed manifest through a `--depth` long reference path (default 5). The objects share the Repositories, each a
different `subPath` of the one registry, so the controller holds that many repository cache entries.

All objects live in one namespace. Before it applies, a Deployer checks the objects of the applied kinds in its namespace
for ApplySet conflicts ([ocm-project#1412](https://github.com/open-component-model/ocm-project/issues/1412)), and with
many Deployers in one namespace that check dominates time and memory and hides the cost of reading the resource. The
registry requests are unaffected by it, so they are the signal to judge a change to how the Deployer reads its resource
by, in every phase. Time and CPU of such a change are reported but not judged.

To find which knob a regression comes from, rerun with that knob turned down: `--depth=0` puts the manifest into the
root, `--versions=1` skips choosing between versions, `--repositories=1` shares one Repository, `--sign=false` drops
signing.

### Signing

The benchmark signs every component version with RSASSA-PSS under the signature name `perf`, with a key
generated for the run. The Components reference a Secret that holds only the public key and a
`signing.config.ocm.software` entry for `perf`, with the `Propagate` policy, so the Resources and Deployers
verify the root as well. The controller verifies the root by signature and every component version below it
against the digest its parent records for it.

The controller accepts a descriptor that misses a resource or reference digest without verifying it at all.
The benchmark therefore writes every image and reference digest, and refuses to sign a component version
that is not safely digestible, the same check `ocm sign cv` makes. Component versions are published level
by level, leaves first, because a reference digest needs the referenced component version.

The NamespacedDeployers apply one ConfigMap each as the `perf-deployer` service account, which a Role in their
namespace allows to write ConfigMaps. The ConfigMap's data holds the release version, so the phases read the deployed
state from the cluster itself, not from a status field.

Objects are created in parallel, the Repositories, the signing Secret and the deployer RBAC first, the way a GitOps tool
applies a directory. A watch records when each object first becomes ready.

The benchmark's own client is limited to 200 requests per second. A run that creates objects for longer than the
controller needs to reconcile them is bound by that limit, not by the controller.

## Phases

A run measures these phases in order, each in a window of its own:

| Phase     | Prepare (not measured)                     | Change                                                    | Done when, per pipeline           | Models                                                                                                      |
|-----------|--------------------------------------------|-----------------------------------------------------------|-----------------------------------|-------------------------------------------------------------------------------------------------------------|
| `rollout` | push the image pool, publish release `1.0` | create all objects                                        | the NamespacedDeployer is `Ready` | the first install, with cold caches                                                                         |
| `drift`   |                                            | set `data.version: drifted` on every deployed ConfigMap   | the ConfigMap is at `1.0.0` again | any update of a deployed object, status churn included, enqueues its Deployer: warm caches, the same digest |
| `update`  | publish release `1.1`                      | set `spec.semver` of every Component to `>=1.1.0 <1.2.0` | the ConfigMap is at `1.1.0`       | a release rolling out: a new digest down the whole reference chain                                          |

The drift phase is a Deployer reading its component version and resource again for an object it already deployed. The
update has to change the Components: publishing alone triggers nothing before their next poll, 30 minutes away.

A release is one published generation of every pipeline's component versions. Each phase publishes its own release in
its own prepare: a release published earlier would add versions the rollout's Components list. Each constraint stops
below the next release, so a Component that polls while the update is prepared stays on its release.

| Release | Root versions                    | Chain, leaf and deployed `data.version` | Constraint        |
|---------|----------------------------------|-----------------------------------------|-------------------|
| `1.0`   | `1.0.0` … `1.0.<--versions - 1>` | `1.0.0`                                 | `>=1.0.0 <1.1.0`  |
| `1.1`   | `1.1.0` … `1.1.<--versions - 1>` | `1.1.0`                                 | `>=1.1.0 <1.2.0`  |

Both carry the same image resources per root. References pin versions, so every component version of the update is new.

The tracker of a phase ignores the objects that existed before its change. A drifted ConfigMap is done at the version it
had before the change, so only seeing it there again after the change counts.

## Options

An invocation runs every `--objects` count `--repeats` times, each run on a fresh namespace and, by default, a restarted
controller.

| Option                 | Default                    | What it changes                                                                                                                                          |
|------------------------|----------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------|
| `--objects`            | `100,500,1000`             | Pipelines per run, one run per count. A pipeline is a Component, a Resource and a NamespacedDeployer.                                                    |
| `--repeats`            | `3`                        | Runs per count. `/perf` compares the medians.                                                                                                            |
| `--versions`           | `5`                        | Root versions per release. The Components select the newest by constraint, so they list and compare all of them.                                         |
| `--depth`              | `5`                        | Reference chain from the root to the component that holds the manifest. Every level is one more component version the Resource and the Deployer resolve. |
| `--resources`          | `100`                      | OCI image resources per root descriptor. Nothing deploys them; they make the descriptor large.                                                           |
| `--repositories`       | `10`                       | Repositories the pipelines share round-robin, each on its own registry sub-path and in its own repository cache entry. Capped at `--objects`.            |
| `--sign`               | `true`                     | Signs every component version; the controller verifies the root signature and every reference digest below it.                                           |
| `--timeout`            | `20m`                      | Per phase: the wait for its objects, and again for the queues to drain. Also bounds the controller restart and the namespace deletion.                   |
| `--sample-interval`    | `2s`                       | Resolution of the sampled peaks and of the settled time, which takes 3 idle samples in a row.                                                            |
| `--restart-controller` | `true`                     | Empty caches for every run. Turn it off to keep a port-forward to the pod, for [profiling](#profiling).                                                  |
| `--keep-namespaces`    | `false`                    | Keeps every run's namespace for inspection; they pile up.                                                                                                |
| `--out`                | `test/performance/results` | Where each invocation writes its timestamped results directory.                                                                                          |

The remaining flags locate the cluster, the registry and the controller: `--context`, `--registry`, `--cluster-registry`,
`--registry-metrics`, `--controller-namespace`, `--controller-selector` and `--metrics-port`. Their defaults match the
setup of `task test/performance/setup/local`.

`--context` defaults to `kind-ocm-perf`, not the current context. The name alone does not prove the cluster is the
benchmark's: a run restarts the controller pods and creates and deletes namespaces wherever the context points, so check it
first.

## Results

Each invocation writes `test/performance/results/<timestamp>/`, which is git-ignored:

- `summary.md`: one row per phase of every run. Image (short digest) and Signed show at a glance whether two runs are
  comparable. The Notes column flags phases that timed out, did not drain or lost the controller, each note led by
  its phase; the benchmark exits non-zero when any run is flagged.
- `<run>/result.json`: the full result, rewritten after every phase. It records the workload knobs, the environment
  (image digest, controller args and resources, Kubernetes version) and `phases`, one entry per measured phase in
  execution order:
  - `outcome`: expected, ready, failed, pending, failure reasons, whether it timed out, did not drain or lost the
    controller. When a phase times out, `notReady` counts the Components, Resources and NamespacedDeployers that were
    not `Ready`, by kind and reason.
  - `durations`: prepare, change, total, settled, first ready, p50/p90/p99.
  - `cost`: registry requests, CPU, memory, counter deltas and sampled gauge peaks.
- `<run>/<phase>/controller-{start,end}.prom`, `<run>/<phase>/registry-{start,end}.prom`: raw scrapes.
- `<run>/<phase>/samples.json`: gauges, cumulative CPU seconds and memory over time, `at` in seconds since the
  phase's measurement started.

All durations but `prepare` count from the moment the phase's change starts, not from each object's own change: the
first object created, the first ConfigMap drifted, the first Component updated. `p50` and `p90` therefore include the
time the benchmark needed to change the objects that came before.

Metric sources:

| Measurement        | Source                                                                                                                                                                                 |
|--------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| CPU seconds        | delta of the controller's `process_cpu_seconds_total`, up to the settled time                                                                                                          |
| Avg cores          | CPU seconds divided by the settled time                                                                                                                                                |
| Peak cores         | highest rate of `process_cpu_seconds_total` between two consecutive samples. It is an average over one `--sample-interval`, so shorter bursts read lower.                              |
| Settled            | first of 3 consecutive samples with every `workqueue_depth` and `controller_runtime_active_workers` at zero, after all objects are done. Counters and cost cover the phase up to here. |
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
go run ./test/performance --objects=1000 --repeats=1 --restart-controller=false &
go tool pprof http://localhost:6060/debug/pprof/heap
```

Profiles cost CPU, so do not use numbers from a profiled run as benchmark results.
