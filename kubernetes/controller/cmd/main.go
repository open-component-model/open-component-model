package main

import (
	"context"
	"crypto/tls"
	"flag"
	"os"
	"time"

	// to ensure that exec-entrypoint and run can make use of them.
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/fluxcd/pkg/runtime/events"
	"github.com/hashicorp/golang-lru/v2/expirable"
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

	"ocm.software/open-component-model/bindings/go/credentials"
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/rsa/signing/handler"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/component"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/deployer"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/deployer/cache"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/deployer/dynamic"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/replication"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/repository"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/resource"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
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
	ocm.MustRegisterMetrics(metrics.Registry)
}

//nolint:funlen,maintidx // the main function is complex enough as it is - we don't want to separate the initialization
func main() {
	var (
		metricsAddr               string
		enableLeaderElection      bool
		probeAddr                 string
		secureMetrics             bool
		enableHTTP2               bool
		eventsAddr                string
		deployerDownloadCacheSize int
		ocmContextCacheSize       int
		ocmSessionCacheSize       int
		resourceConcurrency       int
		resolverWorkerCount       int
		resolverWorkerQueueLength int
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metric endpoint binds to. "+
		"Use the port :8080. If not set, it will be 0 in order to disable the metrics server")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.StringVar(&eventsAddr, "events-addr", "", "The address of the events receiver.")
	flag.IntVar(&deployerDownloadCacheSize, "deployer-download-cache-size", 1_000, //nolint:mnd // no magic number
		"The maximum size of the deployer download object LRU cache.")
	flag.IntVar(&ocmContextCacheSize, "ocm-context-cache-size", 100, //nolint:mnd // no magic number
		"The maximum size of the OCM context cache. This is the number of active OCM contexts that can be kept alive.")
	flag.IntVar(&ocmSessionCacheSize, "ocm-session-cache-size", 100, //nolint:mnd // no magic number
		"The maximum size of the OCM context cache. This is the number of active OCM sessions that can be kept alive.")
	flag.IntVar(&resourceConcurrency, "resource-controller-concurrency", 4, //nolint:mnd // no magic number
		"The resource controller concurrency. This is the number of active resource controller workers that can be kept alive.")
	flag.IntVar(&resolverWorkerCount, "resolver-worker-count", 10, //nolint:mnd // no magic number
		"This is the number of active resolver workers.")
	flag.IntVar(&resolverWorkerQueueLength, "resolver-worker-queue-length", 100, //nolint:mnd // no magic number
		"The maximum number of work items in the queue for the workers to pick up component versions to resolve from.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

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

	pm := manager.NewPluginManager(ctx)
	scheme := ocmruntime.NewScheme()
	scheme.MustRegisterWithAlias(&ociv1.Repository{},
		ocmruntime.NewVersionedType(ociv1.Type, ociv1.Version),
		ocmruntime.NewUnversionedType(ociv1.Type),
		ocmruntime.NewVersionedType(ociv1.ShortType, ociv1.Version),
		ocmruntime.NewUnversionedType(ociv1.ShortType),
		ocmruntime.NewVersionedType(ociv1.ShortType2, ociv1.Version),
		ocmruntime.NewUnversionedType(ociv1.ShortType2),
		ocmruntime.NewVersionedType(ociv1.LegacyRegistryType, ociv1.Version),
		ocmruntime.NewUnversionedType(ociv1.LegacyRegistryType),
		ocmruntime.NewVersionedType(ociv1.LegacyRegistryType2, ociv1.Version),
		ocmruntime.NewUnversionedType(ociv1.LegacyRegistryType2),
	)
	scheme.MustRegisterWithAlias(&ctfv1.Repository{},
		ocmruntime.NewVersionedType(ctfv1.Type, ctfv1.Version),
		ocmruntime.NewUnversionedType(ctfv1.Type),
		ocmruntime.NewVersionedType(ctfv1.ShortType, ctfv1.Version),
		ocmruntime.NewUnversionedType(ctfv1.ShortType),
		ocmruntime.NewVersionedType(ctfv1.ShortType2, ctfv1.Version),
		ocmruntime.NewUnversionedType(ctfv1.ShortType2),
	)
	repositoryProvider := provider.NewComponentVersionRepositoryProvider(provider.WithScheme(scheme))
	if err := pm.ComponentVersionRepositoryRegistry.RegisterInternalComponentVersionRepositoryPlugin(repositoryProvider); err != nil {
		setupLog.Error(err, "failed to register internal component version repository plugin")
		os.Exit(1)
	}
	signingHandler, err := handler.New(signingv1alpha1.Scheme, true)
	if err != nil {
		setupLog.Error(err, "failed to create signing handler")
		os.Exit(1)
	}
	if err := pm.SigningRegistry.RegisterInternalComponentSignatureHandler(signingHandler); err != nil {
		setupLog.Error(err, "failed to register internal signing plugin")
		os.Exit(1)
	}
	if err := pm.CredentialRepositoryRegistry.RegisterInternalCredentialRepositoryPlugin(&ocicredentials.OCICredentialRepository{}, []ocmruntime.Type{credentials.AnyConsumerIdentityType}); err != nil {
		setupLog.Error(err, "failed to register internal credential repository plugin")
		os.Exit(1)
	}

	const unlimited = 0
	ttl := time.Minute * 30
	resolverCache := expirable.NewLRU[string, *workerpool.Result](unlimited, nil, ttl)

	// Create worker pool with its own dependencies
	workerPool := workerpool.NewWorkerPool(workerpool.PoolOptions{
		WorkerCount: resolverWorkerCount,
		QueueSize:   resolverWorkerQueueLength,
		Logger:      &setupLog,
		Client:      mgr.GetClient(),
		Cache:       resolverCache,
	})
	if err := mgr.Add(workerPool); err != nil {
		setupLog.Error(err, "unable to add worker pool")
		os.Exit(1)
	}

	var eventsRecorder *events.Recorder
	if eventsRecorder, err = events.NewRecorder(mgr, ctrl.Log, eventsAddr, "ocm-k8s-toolkit"); err != nil {
		setupLog.Error(err, "unable to create event recorder")
		os.Exit(1)
	}

	ocmContextCache := ocm.NewContextCache("shared_ocm_context_cache", ocmContextCacheSize, ocmSessionCacheSize, mgr.GetClient(), mgr.GetLogger())
	if err := mgr.Add(ocmContextCache); err != nil {
		setupLog.Error(err, "unable to create ocm context cache")
		os.Exit(1)
	}

	resolver := resolution.NewResolver(mgr.GetClient(), &setupLog, workerPool, pm)
	if err = (&repository.Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			EventRecorder: eventsRecorder,
		},
		Resolver: resolver,
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Repository")
		os.Exit(1)
	}
	if err = (&component.Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			EventRecorder: eventsRecorder,
		},
		Resolver:      resolver,
		PluginManager: pm,
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Component")
		os.Exit(1)
	}

	if err = (&resource.Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			EventRecorder: eventsRecorder,
		},
		Resolver:      resolver,
		PluginManager: pm,
	}).SetupWithManager(ctx, mgr, resourceConcurrency); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Resource")
		os.Exit(1)
	}

	if err = (&replication.Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			EventRecorder: eventsRecorder,
		},
		OCMContextCache: ocmContextCache,
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Replication")
		os.Exit(1)
	}

	if err = (&deployer.Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			EventRecorder: eventsRecorder,
		},
		DownloadCache: cache.NewMemoryDigestObjectCache[string, []*unstructured.Unstructured]("deployer_download_cache", deployerDownloadCacheSize, func(k string, v []*unstructured.Unstructured) {
			setupLog.Info("evicting deployment objects from cache", "key", k, "count", len(v))
		}),
		OCMContextCache: ocmContextCache,
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Deployer")
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
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
