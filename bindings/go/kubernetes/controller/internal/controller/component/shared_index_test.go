package component

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	"github.com/hashicorp/golang-lru/v2/expirable"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerconfig "sigs.k8s.io/controller-runtime/pkg/config"
	metricserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/discovery"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/indexes"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution/workerpool"
)

var _ = Describe("Shared controller index", func() {
	DescribeTable("sets up both consumers after bootstrap",
		func(ctx SpecContext, order []string) {
			skipNameValidation := true
			mgr, err := ctrl.NewManager(cfg, ctrl.Options{
				Scheme:                 scheme.Scheme,
				Controller:             controllerconfig.Controller{SkipNameValidation: &skipNameValidation},
				HealthProbeBindAddress: "0",
				Metrics: metricserver.Options{
					BindAddress: "0",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(indexes.RegisterDiscoveryComponentRef(ctx, mgr.GetFieldIndexer())).To(Succeed())

			logger := logr.Discard()
			resolverCache := expirable.NewLRU[string, *workerpool.Result](0, nil, 30*time.Minute)
			workerPool := workerpool.NewWorkerPool(workerpool.PoolOptions{
				WorkerCount: 1,
				QueueSize:   1,
				Logger:      &logger,
				Client:      mgr.GetClient(),
				Cache:       resolverCache,
			})
			Expect(mgr.Add(workerPool)).To(Succeed())
			resolver := resolution.NewResolver(&logger, workerPool)

			for _, controllerName := range order {
				switch controllerName {
				case "Component":
					Expect((&Reconciler{
						BaseReconciler: &ocm.BaseReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()},
						Resolver:       resolver,
					}).SetupWithManager(ctx, mgr)).To(Succeed())
				case "Discovery":
					Expect((&discovery.Reconciler{
						BaseReconciler: &ocm.BaseReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()},
					}).SetupWithManager(ctx, mgr)).To(Succeed())
				default:
					Fail(fmt.Sprintf("unknown controller %q", controllerName))
				}
			}
		},
		Entry("Component then Discovery", []string{"Component", "Discovery"}),
		Entry("Discovery then Component", []string{"Discovery", "Component"}),
	)

	It("serves namespace-scoped indexed lists from the manager cache", func(ctx SpecContext) {
		firstNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "shared-index-first-"}}
		secondNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "shared-index-second-"}}
		Expect(k8sClient.Create(ctx, firstNamespace)).To(Succeed())
		Expect(k8sClient.Create(ctx, secondNamespace)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, firstNamespace))).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, secondNamespace))).To(Succeed())
		})

		for _, discovery := range []*v1alpha1.Discovery{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "matching", Namespace: firstNamespace.Name},
				Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: "component"}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "other-reference", Namespace: firstNamespace.Name},
				Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: "other"}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "same-name-other-namespace", Namespace: secondNamespace.Name},
				Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: "component"}},
			},
		} {
			Expect(k8sClient.Create(ctx, discovery)).To(Succeed())
		}

		Expect(k8sManager.GetCache().WaitForCacheSync(ctx)).To(BeTrue())
		Eventually(func(ctx context.Context) ([]int, error) {
			counts := make([]int, 0, 2)
			for _, namespace := range []string{firstNamespace.Name, secondNamespace.Name} {
				list := &v1alpha1.DiscoveryList{}
				if err := k8sManager.GetClient().List(ctx, list, client.InNamespace(namespace)); err != nil {
					return nil, err
				}
				counts = append(counts, len(list.Items))
			}
			return counts, nil
		}, "10s").WithContext(ctx).Should(Equal([]int{2, 1}))

		Eventually(func(ctx context.Context) ([]string, error) {
			list := &v1alpha1.DiscoveryList{}
			if err := k8sManager.GetClient().List(ctx, list,
				client.InNamespace(firstNamespace.Name),
				client.MatchingFields{indexes.DiscoveryComponentRef: "component"}); err != nil {
				return nil, err
			}
			names := make([]string, 0, len(list.Items))
			for _, item := range list.Items {
				names = append(names, item.Name)
			}
			return names, nil
		}, "10s").WithContext(ctx).Should(ConsistOf("matching"))
	})
})
