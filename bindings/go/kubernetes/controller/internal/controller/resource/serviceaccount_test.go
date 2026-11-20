package resource

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/serviceaccount"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/serviceaccount/serviceaccounttest"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ = Describe("Resource service account", func() {
	const (
		reader        = "ocm-reader"
		componentName = "ocm.software/sa-component"
		resourceName  = "data"
	)

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
		// envtest runs no service account controller, so the default service account has to be created.
		serviceaccounttest.CreateServiceAccount(ctx, k8sClient, namespace, serviceaccount.DefaultName)

		_, specData = test.SetupCTFComponentVersionRepository(ctx, GinkgoT().TempDir(), []*descruntime.Descriptor{{
			Component: descruntime.Component{
				ComponentMeta: descruntime.ComponentMeta{
					ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: "1.0.0"},
				},
				Resources: []descruntime.Resource{{
					ElementMeta: descruntime.ElementMeta{
						ObjectMeta: descruntime.ObjectMeta{Name: resourceName, Version: "1.0.0"},
					},
					Type:     "plainText",
					Relation: descruntime.LocalRelation,
					Access: &ocispec.OCIImage{
						Type:           runtime.Type{Name: "ociArtifact", Version: "v1"},
						ImageReference: "ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.24.0",
					},
				}},
				Provider: descruntime.Provider{Name: "ocm.software"},
			},
		}})
		local = serviceaccounttest.CreateConfigSecret(ctx, k8sClient, namespace)
		foreign = serviceaccounttest.CreateConfigSecret(ctx, k8sClient, other)
	})

	newResource := func(ctx SpecContext, propagated []v1alpha1.OCMConfiguration, mutate func(*v1alpha1.Resource)) *v1alpha1.Resource {
		GinkgoHelper()

		component := test.MockComponent(ctx, "component", namespace, &test.MockComponentOptions{
			Client:     k8sClient,
			Recorder:   recorder,
			Repository: "repository",
			Info: v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        "1.0.0",
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			},
			EffectiveOCMConfig: propagated,
		})

		resource := &v1alpha1.Resource{
			ObjectMeta: metav1.ObjectMeta{Name: "resource", Namespace: namespace},
			Spec: v1alpha1.ResourceSpec{
				ComponentRef: corev1.LocalObjectReference{Name: component.GetName()},
				Resource: v1alpha1.ResourceID{
					ByReference: v1alpha1.ResourceReference{Resource: runtime.Identity{"name": resourceName}},
				},
			},
		}
		mutate(resource)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			test.DeleteObject(ctx, k8sClient, resource)
			test.DeleteObject(ctx, k8sClient, component)
		})

		return resource
	}

	It("denies config a Component propagates from another namespace until default may get it", func(ctx SpecContext) {
		resource := newResource(ctx, []v1alpha1.OCMConfiguration{test.SecretOCMConfiguration(foreign)}, func(*v1alpha1.Resource) {})

		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, resource, v1alpha1.GetConfigurationFailedReason,
			`secrets "creds" is forbidden`)

		serviceaccounttest.GrantOCMConfigReads(ctx, k8sClient, other, namespace, serviceaccount.DefaultName)
		test.WaitForReadyObject(ctx, k8sClient, resource, map[string]any{"Status.Component.Component": componentName})
	})

	It("reads a Secret in its own namespace its service account was not granted", func(ctx SpecContext) {
		serviceaccounttest.CreateServiceAccount(ctx, k8sClient, namespace, reader)
		resource := newResource(ctx, nil, func(r *v1alpha1.Resource) {
			r.Spec.ServiceAccountName = reader
			r.Spec.OCMConfig = []v1alpha1.OCMConfiguration{test.SecretOCMConfiguration(local)}
		})

		test.WaitForReadyObject(ctx, k8sClient, resource, map[string]any{"Status.Component.Component": componentName})
	})
})
