package repository

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/serviceaccount"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/serviceaccount/serviceaccounttest"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
)

var _ = Describe("Repository service account", func() {
	const reader = "ocm-reader"

	var (
		namespace, other string
		specData         []byte
		local, foreign   *corev1.Secret
	)

	BeforeEach(func(ctx SpecContext) {
		namespace = test.SanitizeNameForK8s("sa-" + ctx.SpecReport().LeafNodeText)
		other = test.SanitizeNameForK8s("sa-other-" + ctx.SpecReport().LeafNodeText)
		for _, name := range []string{namespace, other} {
			Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})).To(Succeed())
		}
		// The suite grants the default service account the configuration of its own namespace only.
		serviceaccounttest.WaitForDefaultServiceAccount(ctx, k8sClient, namespace)

		_, specData = test.SetupCTFComponentVersionRepository(ctx, GinkgoT().TempDir(), nil)
		local = serviceaccounttest.CreateConfigSecret(ctx, k8sClient, namespace)
		foreign = serviceaccounttest.CreateConfigSecret(ctx, k8sClient, other)
	})

	newRepository := func(ctx SpecContext, mutate func(*v1alpha1.Repository)) *v1alpha1.Repository {
		GinkgoHelper()

		repository := newTestRepository(namespace, "repository", &specData)
		mutate(repository)
		Expect(k8sClient.Create(ctx, repository)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			test.DeleteObject(ctx, k8sClient, repository)
		})

		return repository
	}

	It("denies a Secret in another namespace until default may get it", func(ctx SpecContext) {
		repository := newRepository(ctx, func(r *v1alpha1.Repository) {
			r.Spec.OCMConfig = []v1alpha1.OCMConfiguration{test.SecretOCMConfiguration(foreign)}
		})

		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, repository, v1alpha1.GetConfigurationFailedReason,
			`secrets "creds" is forbidden`)

		serviceaccounttest.GrantOCMConfigReads(ctx, k8sClient, other, namespace, serviceaccount.DefaultName)
		test.WaitForReadyObject(ctx, k8sClient, repository, map[string]any{})
	})

	It("denies a Secret in its own namespace its service account may not get", func(ctx SpecContext) {
		serviceaccounttest.CreateServiceAccount(ctx, k8sClient, namespace, reader)
		repository := newRepository(ctx, func(r *v1alpha1.Repository) {
			r.Spec.ServiceAccountName = reader
			r.Spec.OCMConfig = []v1alpha1.OCMConfiguration{test.SecretOCMConfiguration(local)}
		})

		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, repository, v1alpha1.GetConfigurationFailedReason,
			fmt.Sprintf("service account %s/%s may not get it", namespace, reader))
	})
})
