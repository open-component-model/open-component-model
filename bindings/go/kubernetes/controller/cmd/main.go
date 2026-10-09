package main

import (
	"context"
	"crypto/fips140"
	"crypto/tls"
	"flag"
	"log/slog"
	"os"

	// to ensure that exec-entrypoint and run can make use of them.
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/go-logr/logr"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/component"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/deployer"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/deployer/cache"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/deployer/dynamic"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/discovery"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/replication"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/repository"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/resource"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

const (
	// defaultMaxResourceSize is a generous upper bound for Kubernetes manifests.
	// etcd's default --max-request-bytes is 1.5 MiB (https://etcd.io/docs/v3.5/dev-guide/limit/),
	// so a multi-document manifest written to the cluster will always stay well under 2 MiB.
	defaultMaxResourceSize = "2Mi"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme

	dynamic.MustRegisterMetrics(metrics.Registry)
	cache.MustRegisterMetrics(metrics.Registry)
}

//nolint:funlen,maintidx // the main function is complex enough as it is - we don't want to separate the initialization
func main() {
	var (
		metricsAddr               string
		enableLeaderElection      bool
		probeAddr                 string
		pprofAddr                 string
		secureMetrics             bool
		enableHTTP2               bool
		deployerDownloadCacheSize int
		deployerMaxResourceSize   string
		resourceConcurrency       int
		replicationConcurrency    int
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metric endpoint binds to. "+
		"Use the port :8080. If not set, it will be 0 in order to disable the metrics server")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&pprofAddr, "pprof-bind-address", ":6060",
		"The address the pprof endpoint binds to (only used in builds with the pprof tag).")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.IntVar(&deployerDownloadCacheSize, "deployer-download-cache-size", 1_000, //nolint:mnd // no magic number
		"The maximum size of the deployer download object LRU cache.")
	flag.StringVar(&deployerMaxResourceSize, "deployer-download-max-resource-size", defaultMaxResourceSize,
		"Maximum size of a single downloadable resource as a Kubernetes resource.Quantity (e.g. \"2Mi\", \"512Ki\"). \"0\" disables the limit.")
	flag.IntVar(&resourceConcurrency, "resource-controller-concurrency", 4, //nolint:mnd // no magic number
		"The resource controller concurrency. This is the number of active resource controller workers that can be kept alive.")
	flag.IntVar(&replicationConcurrency, "replication-controller-concurrency", 4, //nolint:mnd // no magic number
		"The replication controller concurrency. This is the number of active replication controller workers that can be kept alive.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	maxResourceQuantity, err := apiresource.ParseQuantity(deployerMaxResourceSize)
	if err != nil {
		setupLog.Error(err, "invalid flag value", "flag", "deployer-download-max-resource-size", "value", deployerMaxResourceSize)
		os.Exit(1)
	}
	maxResourceSizeBytes := maxResourceQuantity.Value()
	if maxResourceSizeBytes < 0 {
		setupLog.Error(nil, "invalid flag value", "flag", "deployer-download-max-resource-size", "value", deployerMaxResourceSize, "reason", "must be >= 0")
		os.Exit(1)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog.Info("FIPS 140-3 mode", "enabled", fips140.Enabled(), "module", fips140.Version())

	ctx := context.Background()

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "56490b8c.ocm.software",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,

	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if pprofEnabled {
		setupLog.Info("starting pprof server", "address", pprofAddr)
		startPprof(pprofAddr)
	}

	ocirepository.MustAddLegacyToScheme(ocirepository.Scheme)

	// Plugins use object config so they need to be request based.
	pluginLogger := slog.New(logr.ToSlogHandler(setupLog))
	// The OCI caches own their directory on disk, so the per-request plugin managers share one instance.
	ociCaches := setup.NewOCICaches("")
	newPluginManager := func(ctx context.Context, cfg *genericv1.Config) (*manager.PluginManager, error) {
		return setup.NewPluginManager(ctx, cfg, pluginLogger, setup.WithOCICaches(ociCaches))
	}

	if _, err := newPluginManager(ctx, nil); err != nil {
		setupLog.Error(err, "failed to create plugin manager")
		os.Exit(1)
	}

	// TODO: migrate to mgr.GetEventRecorder() once BaseReconciler uses events.EventRecorder
	eventsRecorder := mgr.GetEventRecorderFor("ocm-k8s-toolkit") //nolint:staticcheck,nolintlint

	resolver := resolution.NewResolver(&setupLog)
	if err = (&repository.Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:           mgr.GetClient(),
			Scheme:           mgr.GetScheme(),
			EventRecorder:    eventsRecorder,
			NewPluginManager: newPluginManager,
		},
		Resolver: resolver,
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Repository")
		os.Exit(1)
	}
	if err = (&component.Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:           mgr.GetClient(),
			Scheme:           mgr.GetScheme(),
			EventRecorder:    eventsRecorder,
			NewPluginManager: newPluginManager,
		},
		Resolver: resolver,
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Component")
		os.Exit(1)
	}

	if err = (&resource.Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:           mgr.GetClient(),
			Scheme:           mgr.GetScheme(),
			EventRecorder:    eventsRecorder,
			NewPluginManager: newPluginManager,
		},
		Resolver: resolver,
	}).SetupWithManager(ctx, mgr, resourceConcurrency); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Resource")
		os.Exit(1)
	}

	if err = (&replication.Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:           mgr.GetClient(),
			Scheme:           mgr.GetScheme(),
			EventRecorder:    eventsRecorder,
			NewPluginManager: newPluginManager,
		},
		Resolver:         resolver,
		RepositoryScheme: ocirepository.Scheme,
	}).SetupWithManager(ctx, mgr, replicationConcurrency); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Replication")
		os.Exit(1)
	}

	if err = (&discovery.Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:           mgr.GetClient(),
			Scheme:           mgr.GetScheme(),
			EventRecorder:    eventsRecorder,
			NewPluginManager: newPluginManager,
		},
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Discovery")
		os.Exit(1)
	}

	if err = (&deployer.Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:           mgr.GetClient(),
			Scheme:           mgr.GetScheme(),
			EventRecorder:    eventsRecorder,
			NewPluginManager: newPluginManager,
		},
		DownloadCache: cache.NewMemoryDigestObjectCache[string, []*unstructured.Unstructured]("deployer_download_cache", deployerDownloadCacheSize, func(k string, v []*unstructured.Unstructured) {
			setupLog.Info("evicting deployment objects from cache", "key", k, "count", len(v))
		}),
		Resolver:             resolver,
		MaxResourceSizeBytes: maxResourceSizeBytes,
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Deployer")
		os.Exit(1)
	}
	if err = (&v1alpha1.Component{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Component")
		os.Exit(1)
	}
	if err = (&v1alpha1.Deployer{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Deployer")
		os.Exit(1)
	}
	if err = (&v1alpha1.Repository{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Repository")
		os.Exit(1)
	}
	if err = (&v1alpha1.Resource{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Resource")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	go func() {
		// Block until our controller manager is elected leader. We presume our
		// entire process will terminate if we lose leadership, so we don't need
		// to handle that.
		<-mgr.Elected()
	}()

	setupLog.Info("starting manager")
	err = mgr.Start(ctrl.SetupSignalHandler())
	ociCaches.Close()
	if err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
