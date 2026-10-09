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
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type options struct {
	kubeContext         string
	scenarios           []scenario
	sizes               []int
	repeats             int
	versions            int
	depth               int
	resources           int
	registryURL         string
	clusterRegistryURL  string
	registryMetricsURL  string
	controllerNamespace string
	controllerSelector  string
	metricsPort         int
	timeout             time.Duration
	sampleInterval      time.Duration
	outDir              string
	restartController   bool
	keepNamespaces      bool
}

func main() {
	if err := run(); err != nil {
		slog.Error("benchmark failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
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

	r := &runner{c: c, opts: opts, outDir: outDir}
	var results []*result
	for _, s := range opts.scenarios {
		for _, n := range opts.sizes {
			for rep := 1; rep <= opts.repeats; rep++ {
				res, err := r.runOnce(ctx, s, n, rep)
				if err != nil {
					return fmt.Errorf("%s with %d objects, repeat %d: %w", s, n, rep, err)
				}
				results = append(results, res)
				if err := writeSummary(outDir, results); err != nil {
					return err
				}
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
	var scenarios, sizes string

	fs := flag.NewFlagSet("performance", flag.ContinueOnError)
	fs.StringVar(&opts.kubeContext, "context", "kind-ocm-perf",
		"Kubeconfig context. Defaults to the benchmark cluster so a run never lands on a real cluster by accident.")
	fs.StringVar(&scenarios, "scenarios", joinScenarios(allScenarios), "Comma-separated scenarios. See README.md for what each one creates.")
	fs.IntVar(&opts.versions, "versions", defaultVersions, "Versions of the component in the versions scenario.")
	fs.IntVar(&opts.depth, "depth", defaultDepth, "Length of the reference chain in the nested and complex scenarios.")
	fs.IntVar(&opts.resources, "resources", defaultResources, "Resources per root component in the complex scenario.")
	fs.StringVar(&sizes, "objects", "100,500,1000", "Comma-separated object counts per run.")
	fs.IntVar(&opts.repeats, "repeats", 3, "Repetitions of every scenario and object count.")
	fs.StringVar(&opts.registryURL, "registry", "http://localhost:5555", "Registry URL the benchmark pushes component versions to.")
	fs.StringVar(&opts.clusterRegistryURL, "cluster-registry", "http://ocm-perf-registry:5000",
		"The same registry as seen from inside the cluster; used in the Repository spec.")
	fs.StringVar(&opts.registryMetricsURL, "registry-metrics", "http://localhost:5556/metrics", "Registry Prometheus endpoint.")
	fs.StringVar(&opts.controllerNamespace, "controller-namespace", "ocm-k8s-toolkit-system", "Namespace of the controller deployment.")
	fs.StringVar(&opts.controllerSelector, "controller-selector", "app.kubernetes.io/name=ocm-k8s-toolkit",
		"Label selector matching the controller pod.")
	fs.IntVar(&opts.metricsPort, "metrics-port", 8080, "Port of the controller metrics endpoint.")
	fs.DurationVar(&opts.timeout, "timeout", 20*time.Minute, "Maximum time one run waits for its objects.")
	fs.DurationVar(&opts.sampleInterval, "sample-interval", 2*time.Second, "Interval between gauge and memory samples.")
	fs.StringVar(&opts.outDir, "out", "test/performance/results", "Directory for results; each invocation adds a timestamped subdirectory.")
	fs.BoolVar(&opts.restartController, "restart-controller", true, "Restart the controller before each run so caches start empty.")
	fs.BoolVar(&opts.keepNamespaces, "keep-namespaces", false, "Keep run namespaces for inspection instead of deleting them.")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}

	var err error
	if opts.scenarios, err = parseScenarios(scenarios); err != nil {
		return opts, err
	}
	if opts.sizes, err = parseSizes(sizes); err != nil {
		return opts, err
	}
	if opts.repeats < 1 {
		return opts, fmt.Errorf("repeats must be at least 1, got %d", opts.repeats)
	}
	if opts.versions < 1 || opts.depth < 1 || opts.resources < 1 {
		return opts, fmt.Errorf("versions, depth and resources must be at least 1, got %d, %d and %d",
			opts.versions, opts.depth, opts.resources)
	}
	return opts, nil
}

func parseScenarios(s string) ([]scenario, error) {
	var out []scenario
	for part := range strings.SplitSeq(s, ",") {
		sc := scenario(strings.TrimSpace(part))
		if !slices.Contains(allScenarios, sc) {
			return nil, fmt.Errorf("unknown scenario %q, want one of %s", sc, joinScenarios(allScenarios))
		}
		out = append(out, sc)
	}
	return out, nil
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

func joinScenarios(ss []scenario) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = string(s)
	}
	return strings.Join(parts, ",")
}
