package replication

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
)

// +kubebuilder:scaffold:imports

var (
	cfg        *rest.Config
	k8sClient  client.Client
	k8sManager ctrl.Manager
	testEnv    *envtest.Environment
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Replication Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "bin", "gen", "crd")},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "bin", "k8s",
			fmt.Sprintf("%s-%s-%s", os.Getenv("ENVTEST_K8S_VERSION"), runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(v1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())

	// +kubebuilder:scaffold:scheme
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	komega.SetClient(k8sClient)

	gracefulTimeout := 5 * time.Second
	k8sManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                  scheme.Scheme,
		GracefulShutdownTimeout: &gracefulTimeout,
		Metrics: metricserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).ToNot(HaveOccurred())

	ctx, cancel := context.WithCancel(context.Background())

	events := make(chan string)
	recorder := &record.FakeRecorder{
		Events:        events,
		IncludeObject: true,
	}

	go func() {
		for {
			select {
			case event := <-events:
				GinkgoLogr.Info("Event received", "event", event)
			case <-ctx.Done():
				return
			}
		}
	}()

	pm := manager.NewPluginManager(ctx)
	ocmScheme := ocmruntime.NewScheme()
	ocmScheme.MustRegisterWithAlias(&ctfv1.Repository{},
		ocmruntime.NewVersionedType(ctfv1.Type, ctfv1.Version),
		ocmruntime.NewUnversionedType(ctfv1.Type),
		ocmruntime.NewVersionedType(ctfv1.ShortType, ctfv1.Version),
		ocmruntime.NewUnversionedType(ctfv1.ShortType),
		ocmruntime.NewVersionedType(ctfv1.ShortType2, ctfv1.Version),
		ocmruntime.NewUnversionedType(ctfv1.ShortType2),
	)
	repositoryProvider := provider.NewComponentVersionRepositoryProvider(provider.WithScheme(ocmScheme))
	Expect(pm.ComponentVersionRepositoryRegistry.RegisterInternalComponentVersionRepositoryPlugin(repositoryProvider)).To(Succeed())

	const unlimited = 0
	ttl := time.Minute * 30
	resolverCache := expirable.NewLRU[string, *workerpool.Result](unlimited, nil, ttl)

	workerLogger := logf.Log.WithName("worker-pool")
	workerPool := workerpool.NewWorkerPool(workerpool.PoolOptions{
		WorkerCount: 10,
		QueueSize:   100,
		Logger:      &workerLogger,
		Client:      k8sManager.GetClient(),
		Cache:       resolverCache,
	})
	Expect(k8sManager.Add(workerPool)).To(Succeed())

	resolutionLogger := logf.Log.WithName("resolution")
	resolver := resolution.NewResolver(&resolutionLogger, workerPool, pm)

	Expect((&Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:        k8sManager.GetClient(),
			Scheme:        testEnv.Scheme,
			EventRecorder: recorder,
		},
		Resolver:         resolver,
		PluginManager:    pm,
		RepositoryScheme: ocmScheme,
	}).SetupWithManager(ctx, k8sManager, 1)).To(Succeed())

	mgrDone := make(chan struct{})
	go func() {
		defer GinkgoRecover()
		defer close(mgrDone)
		Expect(k8sManager.Start(ctx)).To(Or(Succeed(), MatchError(ContainSubstring("grace period"))))
	}()

	DeferCleanup(func() {
		cancel()
		<-mgrDone
		Expect(testEnv.Stop()).To(Succeed())
	})
})
