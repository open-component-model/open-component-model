package externalartifact

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

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ocires "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/rsa/signing/handler"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
)

// +kubebuilder:scaffold:imports

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	k8sClient  client.Client
	k8sManager ctrl.Manager
	testEnv    *envtest.Environment
	recorder   record.EventRecorder
	ctx        context.Context
	cancel     context.CancelFunc
	pm         *manager.PluginManager
	testStore  *Storage
)

func TestControllers(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)

	RunSpecs(t, "ExternalArtifact Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "bin", "k8s",
			fmt.Sprintf("%s-%s-%s", os.Getenv("ENVTEST_K8S_VERSION"), runtime.GOOS, runtime.GOARCH)),
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(v1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(sourcev1.AddToScheme(scheme.Scheme)).Should(Succeed())

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

	ctx, cancel = context.WithCancel(GinkgoT().Context())

	events := make(chan string, 100) //nolint:mnd // buffered event channel for the fake recorder
	recorder = &record.FakeRecorder{
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

	// Setup plugin manager (mirrors the resource/deployer suites).
	pm = manager.NewPluginManager(ctx)
	repositoryProvider := provider.NewComponentVersionRepositoryProvider(provider.WithScheme(repository.Scheme))
	Expect(pm.ComponentVersionRepositoryRegistry.RegisterInternalComponentVersionRepositoryPlugin(repositoryProvider)).To(Succeed())
	signingHandler, err := handler.New(signingv1alpha1.Scheme, true)
	Expect(err).ToNot(HaveOccurred())
	Expect(pm.SigningRegistry.RegisterInternalComponentSignatureHandler(signingHandler)).To(Succeed())
	Expect(pm.CredentialRepositoryRegistry.RegisterInternalCredentialRepositoryPlugin(
		&ocicredentials.OCICredentialRepository{},
		[]ocmruntime.Type{v1.Type},
	)).To(Succeed())

	ociResourceRepoPlugin := ocires.NewResourceRepository(&filesystemv1alpha1.Config{})
	Expect(pm.ResourcePluginRegistry.RegisterInternalResourcePlugin(ociResourceRepoPlugin)).To(Succeed())
	Expect(pm.DigestProcessorRegistry.RegisterInternalDigestProcessorPlugin(ociResourceRepoPlugin)).To(Succeed())

	const unlimited = 0
	ttl := time.Minute * 30
	resolverCache := expirable.NewLRU[string, *workerpool.Result](unlimited, nil, ttl)

	workerLogger := logf.Log.WithName("worker-pool")
	workerPool := workerpool.NewWorkerPool(workerpool.PoolOptions{
		WorkerCount: 10,  //nolint:mnd // test worker count
		QueueSize:   100, //nolint:mnd // test queue size
		Logger:      &workerLogger,
		Client:      k8sManager.GetClient(),
		Cache:       resolverCache,
	})
	Expect(k8sManager.Add(workerPool)).To(Succeed())

	resolutionLogger := logf.Log.WithName("resolution")
	resolver := resolution.NewResolver(&resolutionLogger, workerPool, pm)

	// Storage rooted at a temp dir; the advertise host is arbitrary for tests.
	storageDir := GinkgoT().TempDir()
	testStore, err = NewStorage(storageDir, "ocm-k8s-toolkit.ocm-system.svc.cluster.local.:9091")
	Expect(err).NotTo(HaveOccurred())

	Expect((&Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:        k8sManager.GetClient(),
			Scheme:        testEnv.Scheme,
			EventRecorder: recorder,
		},
		Resolver:      resolver,
		PluginManager: pm,
		Storage:       testStore,
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
