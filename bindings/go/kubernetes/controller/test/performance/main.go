// Command performance benchmarks the OCM controllers against a Kind cluster
// set up by `task test/performance/setup/local`. See README.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// options configure one invocation: every object count in sizes, repeats times each.
// The --help text of each flag says what it changes in the measurement.
type options struct {
	// kubeContext defaults to the benchmark's Kind cluster rather than the kubeconfig's current context.
	kubeContext string

	// The invocation matrix: one run per object count and repeat.
	sizes   []int
	repeats int

	// The workload knobs: the shape of every pipeline's component versions, see workload.
	versions     int
	depth        int
	resources    int
	repositories int
	sign         bool

	// Where the registry and the controller are, from the host and from inside the cluster.
	registryURL         string
	clusterRegistryURL  string
	registryMetricsURL  string
	controllerNamespace string
	controllerSelector  string
	metricsPort         int

	// How a run is measured and left behind.
	timeout           time.Duration
	sampleInterval    time.Duration
	outDir            string
	restartController bool
	keepNamespaces    bool
}

func main() {
	if err := benchmark(); err != nil {
		slog.Error("benchmark failed", "error", err)
		os.Exit(1)
	}
}

func benchmark() error {
	// Without a logger controller-runtime prints a stack trace on first use.
	ctrllog.SetLogger(logr.FromSlogHandler(slog.Default().Handler()))

	opts, err := parseFlags(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	// Cleanup outlives the first signal, so hand the next one back to the
	// default handler: a second Ctrl-C then kills the process.
	context.AfterFunc(ctx, cancel)

	cfg, err := config.GetConfigWithContext(opts.kubeContext)
	if err != nil {
		return fmt.Errorf("loading kubeconfig context %q: %w", opts.kubeContext, err)
	}
	c, err := newCluster(cfg, opts)
	if err != nil {
		return err
	}

	outDir := filepath.Join(opts.outDir, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return err
	}
	slog.Info("writing results", "dir", outDir)

	r := &runner{cluster: c, opts: opts, outDir: outDir}
	var results []*result
	for _, n := range opts.sizes {
		for rep := 1; rep <= opts.repeats; rep++ {
			res, err := r.runOnce(ctx, n, rep)
			if err != nil {
				return fmt.Errorf("%d objects, repeat %d: %w", n, rep, err)
			}
			results = append(results, res)
			if err := writeSummary(outDir, results); err != nil {
				return err
			}
		}
	}

	invalid := 0
	for _, res := range results {
		if len(res.notes()) > 0 {
			invalid++
		}
	}
	if invalid > 0 {
		return fmt.Errorf("%d of %d runs are not usable, see %s", invalid, len(results), filepath.Join(outDir, "summary.md"))
	}
	return nil
}

func parseFlags(args []string) (options, error) {
	var opts options
	var sizes string

	fs := flag.NewFlagSet("performance", flag.ContinueOnError)
	fs.StringVar(&opts.kubeContext, "context", "kind-ocm-perf",
		"Kubeconfig context. Defaults to the benchmark cluster, not the current context. A run restarts the controller and creates and deletes namespaces there, so check where the context points.")

	fs.StringVar(&sizes, "objects", "100,500,1000",
		"Comma-separated object counts, one run each. An object is one pipeline: a Component, a Resource and a NamespacedDeployer.")
	fs.IntVar(&opts.repeats, "repeats", 3, "Runs of every object count. /perf compares the medians of the repeats.")

	fs.IntVar(&opts.versions, "versions", defaultVersions,
		"Versions of every root component per release. The Components select the newest by constraint, so they list and compare all of them; 1 leaves nothing to choose.")
	fs.IntVar(&opts.depth, "depth", defaultDepth,
		"Length of the reference chain from the root component to the one holding the manifest. Every level is one more component version the Resource and the Deployer resolve; 0 puts the manifest into the root.")
	fs.IntVar(&opts.resources, "resources", defaultResources,
		"OCI image resources in every root descriptor. Nothing deploys them; they make every root descriptor large.")
	fs.IntVar(&opts.repositories, "repositories", defaultRepositories,
		"Maximum number of Repositories, each on its own registry sub-path, shared round-robin by the pipelines. The controller holds a repository cache entry for each.")
	fs.BoolVar(&opts.sign, "sign", true,
		"Sign every component version and have the controller verify the root signature and every reference digest below it.")

	fs.StringVar(&opts.registryURL, "registry", "http://localhost:5555", "Registry URL the benchmark pushes component versions to.")
	fs.StringVar(&opts.clusterRegistryURL, "cluster-registry", "http://ocm-perf-registry:5000",
		"The same registry as seen from inside the cluster; used in the Repository spec.")
	fs.StringVar(&opts.registryMetricsURL, "registry-metrics", "http://localhost:5556/metrics",
		"Registry Prometheus endpoint, scraped for the registry requests of every phase.")
	fs.StringVar(&opts.controllerNamespace, "controller-namespace", "ocm-k8s-toolkit-system", "Namespace of the controller deployment.")
	fs.StringVar(&opts.controllerSelector, "controller-selector", "app.kubernetes.io/name=ocm-k8s-toolkit",
		"Label selector matching the controller pod.")
	fs.IntVar(&opts.metricsPort, "metrics-port", 8080, "Port of the controller metrics endpoint.")

	fs.DurationVar(&opts.timeout, "timeout", 20*time.Minute,
		"Maximum time one phase waits for its objects, and again for the controller queues to drain. Also bounds the controller restart and the namespace deletion.")
	fs.DurationVar(&opts.sampleInterval, "sample-interval", 2*time.Second,
		"Interval between gauge and memory samples. It is the resolution of the settled time, which takes 3 idle samples in a row, and of the peaks.")
	fs.StringVar(&opts.outDir, "out", "test/performance/results", "Directory for results; each invocation adds a timestamped subdirectory.")
	fs.BoolVar(&opts.restartController, "restart-controller", true,
		"Restart the controller before each run so caches start empty. Turn it off to keep a port-forward to the pod, e.g. for profiling.")
	fs.BoolVar(&opts.keepNamespaces, "keep-namespaces", false,
		"Keep run namespaces for inspection instead of deleting them. They pile up, one per run.")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}

	var err error
	if opts.sizes, err = parseSizes(sizes); err != nil {
		return opts, err
	}
	if opts.repeats < 1 {
		return opts, fmt.Errorf("repeats must be at least 1, got %d", opts.repeats)
	}
	if opts.versions < 1 || opts.resources < 1 || opts.repositories < 1 {
		return opts, fmt.Errorf("versions, resources and repositories must be at least 1, got %d, %d and %d",
			opts.versions, opts.resources, opts.repositories)
	}
	if opts.depth < 0 {
		return opts, fmt.Errorf("depth must not be negative, got %d", opts.depth)
	}
	return opts, nil
}

func parseSizes(s string) ([]int, error) {
	var out []int
	for part := range strings.SplitSeq(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 {
			return nil, fmt.Errorf("invalid object count %q", part)
		}
		out = append(out, n)
	}
	return out, nil
}
