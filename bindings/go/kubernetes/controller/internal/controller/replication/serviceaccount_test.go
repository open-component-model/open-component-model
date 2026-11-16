package replication

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/serviceaccount"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/serviceaccount/serviceaccounttest"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
)

var _ = Describe("Replication service account", func() {
	const (
		componentName = "ocm.software/sa-component"
		forbidden     = `secrets "creds" is forbidden`
	)

	var (
		namespace, other string
		component        *v1alpha1.Component
		targetRepository *v1alpha1.Repository
		foreign          *corev1.Secret
	)

	BeforeEach(func(ctx SpecContext) {
		namespace = test.SanitizeNameForK8s("sa-" + ctx.SpecReport().LeafNodeText)
		other = test.SanitizeNameForK8s("sa-other-" + ctx.SpecReport().LeafNodeText)
		for _, name := range []string{namespace, other} {
			Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})).To(Succeed())
		}
		// The suite grants the default service account the configuration of its own namespace only.
		serviceaccounttest.WaitForDefaultServiceAccount(ctx, k8sClient, namespace)
		foreign = serviceaccounttest.CreateConfigSecret(ctx, k8sClient, other)

		_, sourceSpecData := test.SetupCTFComponentVersionRepository(ctx, GinkgoT().TempDir(), []*descruntime.Descriptor{{
			Meta: descruntime.Meta{Version: "v2"},
			Component: descruntime.Component{
				ComponentMeta: descruntime.ComponentMeta{
					ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: "1.0.0"},
				},
				Provider: descruntime.Provider{Name: "ocm.software"},
			},
		}})
		_, targetSpecData := test.SetupCTFComponentVersionRepository(ctx, GinkgoT().TempDir(), nil)

		component = test.MockComponent(ctx, "source-component", namespace, &test.MockComponentOptions{
			Client:     k8sClient,
			Recorder:   record.NewFakeRecorder(32),
			Repository: "source-repository",
			Info: v1alpha1.ComponentInfo{
				RepositorySpec: &apiextensionsv1.JSON{Raw: sourceSpecData},
				Component:      componentName,
				Version:        "1.0.0",
				Digest: &v2.Digest{
					HashAlgorithm:          "SHA-256",
					NormalisationAlgorithm: "jsonNormalisation/v4alpha1",
					Value:                  "deadbeef",
				},
			},
		})
		targetRepository = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace, "target-repository", targetSpecData)
	})

	newReplication := func(ctx SpecContext, configs []v1alpha1.OCMConfiguration) *v1alpha1.Replication {
		GinkgoHelper()

		replication := &v1alpha1.Replication{
			ObjectMeta: metav1.ObjectMeta{Name: "replication", Namespace: namespace},
			Spec: v1alpha1.ReplicationSpec{
				ComponentRef:        corev1.LocalObjectReference{Name: component.GetName()},
				TargetRepositoryRef: corev1.LocalObjectReference{Name: targetRepository.GetName()},
				OCMConfig:           configs,
			},
		}
		Expect(k8sClient.Create(ctx, replication)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			test.DeleteObject(ctx, k8sClient, replication)
			test.DeleteObject(ctx, k8sClient, component)
			test.DeleteObject(ctx, k8sClient, targetRepository)
		})

		return replication
	}

	It("denies a Secret in another namespace until default may get it", func(ctx SpecContext) {
		replication := newReplication(ctx, []v1alpha1.OCMConfiguration{test.SecretOCMConfiguration(foreign)})

		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, replication, v1alpha1.GetConfigurationFailedReason, forbidden)

		serviceaccounttest.GrantOCMConfigReads(ctx, k8sClient, other, namespace, serviceaccount.DefaultName)
		test.WaitForReadyObject(ctx, k8sClient, replication, map[string]any{"Status.LastTransferredVersion": "1.0.0"})
	})

	It("denies config of the target Repository it may not get", func(ctx SpecContext) {
		// The target's configuration is not part of the effective configuration, so it is authorized on its own.
		targetRepository.Status.EffectiveOCMConfig = []v1alpha1.OCMConfiguration{test.SecretOCMConfiguration(foreign)}
		Expect(k8sClient.Status().Update(ctx, targetRepository)).To(Succeed())

		replication := newReplication(ctx, nil)

		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, replication, v1alpha1.GetConfigurationFailedReason, forbidden)
	})
})
