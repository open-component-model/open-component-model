package replication

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	metricserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
)

// +kubebuilder:scaffold:imports

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var k8sManager ctrl.Manager
var testEnv *envtest.Environment
var recorder record.EventRecorder
var ocmContextCache *ocm.ContextCache

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "bin", "k8s",
			fmt.Sprintf("%s-%s-%s", os.Getenv("ENVTEST_K8S_VERSION"), runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())
	DeferCleanup(testEnv.Stop)

	err = v1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	komega.SetClient(k8sClient)

	k8sManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).ToNot(HaveOccurred())

	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)

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

	ocmContextCache = ocm.NewContextCache("shared_ocm_context_cache", 100, 100, k8sManager.GetClient(), GinkgoLogr)
	Expect(k8sManager.Add(ocmContextCache)).To(Succeed())

	Expect((&Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:        k8sManager.GetClient(),
			Scheme:        testEnv.Scheme,
			EventRecorder: recorder,
		},
		OCMContextCache: ocmContextCache,
	}).SetupWithManager(ctx, k8sManager)).To(Succeed())

	go func() {
		defer GinkgoRecover()
		Expect(k8sManager.Start(ctx)).To(Succeed())
	}()
})
