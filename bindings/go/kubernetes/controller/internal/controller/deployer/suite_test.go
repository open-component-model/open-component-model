package deployer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/deployer/cache"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

// +kubebuilder:scaffold:imports

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	k8sClient     client.Client
	k8sManager    ctrl.Manager
	testEnv       *envtest.Environment
	recorder      record.EventRecorder
	ctx           context.Context
	cancel        context.CancelFunc
	pm            *manager.PluginManager
	downloadCache *cache.MemoryDigestObjectCache[string, []*unstructured.Unstructured]
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	// Get external CRD for the ResourceGraphDefinition
	resp, err := http.Get("https://raw.githubusercontent.com/kro-run/kro/refs/tags/v0.2.3/config/crd/bases/kro.run_resourcegraphdefinitions.yaml")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() error {
		return resp.Body.Close()
	})

	crdBytes, err := io.ReadAll(resp.Body)
	Expect(err).NotTo(HaveOccurred())

	rgdCRD := &apiextensionsv1.CustomResourceDefinition{}
	err = yaml.Unmarshal(crdBytes, rgdCRD)
	Expect(err).NotTo(HaveOccurred())

	testEnv = test.NewEnvTest()
	testEnv.CRDs = []*apiextensionsv1.CustomResourceDefinition{rgdCRD}

	// cfg is defined in this file globally.
	cfg, err := testEnv.Start()
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

	ctx, cancel = context.WithCancel(context.Background())

	events := make(chan string)
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

	// The plugins the controller registers in production, so downloads cover the same
	// access types.
	pm, err = setup.NewPluginManager(ctx, nil, slog.New(logr.ToSlogHandler(GinkgoLogr)), setup.WithTempDir(GinkgoT().TempDir()))
	Expect(err).ToNot(HaveOccurred())

	resolutionLogger := logf.Log.WithName("resolution")
	resolver := resolution.NewResolver(&resolutionLogger)

	downloadCache = cache.NewMemoryDigestObjectCache[string, []*unstructured.Unstructured]("deployer_test_object_cache", 1_000, func(k string, v []*unstructured.Unstructured) {
		GinkgoLogr.Info("DownloadCache eviction", "key", k, "value", fmt.Sprintf("%d objects", len(v)))
	})

	for _, namespaced := range []bool{false, true} {
		Expect((&Reconciler{
			BaseReconciler: &ocm.BaseReconciler{
				Client:           k8sManager.GetClient(),
				Scheme:           testEnv.Scheme,
				EventRecorder:    recorder,
				NewPluginManager: test.StaticPluginManager(pm),
			},
			Namespaced:           namespaced,
			DownloadCache:        downloadCache,
			Resolver:             resolver,
			MaxResourceSizeBytes: 2 * 1024 * 1024,
		}).SetupWithManager(ctx, k8sManager, 1)).To(Succeed())
	}

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
