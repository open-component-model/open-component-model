package component

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/serviceaccount"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
)

var _ = Describe("Component service account", func() {
	const reader = "ocm-reader"

	var (
		namespace, other string
		repositoryObj    *v1alpha1.Repository
		local, foreign   *corev1.Secret
		forbiddenForeign = `secrets "creds" is forbidden`
	)

	BeforeEach(func(ctx SpecContext) {
		namespace = test.SanitizeNameForK8s("sa-" + ctx.SpecReport().LeafNodeText)
		other = test.SanitizeNameForK8s("sa-other-" + ctx.SpecReport().LeafNodeText)
		for _, name := range []string{namespace, other} {
			Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})).To(Succeed())
		}

		_, specData := test.SetupCTFComponentVersionRepository(ctx, GinkgoT().TempDir(), []*descruntime.Descriptor{{
			Component: descruntime.Component{
				ComponentMeta: descruntime.ComponentMeta{
					ObjectMeta: descruntime.ObjectMeta{Name: "ocm.software/sa-component", Version: Version1},
				},
				Provider: descruntime.Provider{Name: "ocm.software"},
			},
		}})
		repositoryObj = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace, "repository", specData)

		local = createConfigSecret(ctx, namespace)
		foreign = createConfigSecret(ctx, other)
	})

	newComponent := func(ctx SpecContext, mutate func(*v1alpha1.Component)) *v1alpha1.Component {
		GinkgoHelper()

		component := &v1alpha1.Component{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: ComponentObj},
			Spec: v1alpha1.ComponentSpec{
				RepositoryRef: corev1.LocalObjectReference{Name: repositoryObj.GetName()},
				Component:     "ocm.software/sa-component",
				Semver:        Version1,
				Interval:      metav1.Duration{Duration: 10 * time.Minute},
			},
		}
		mutate(component)
		Expect(k8sClient.Create(ctx, component)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			test.DeleteObject(ctx, k8sClient, component)
			test.DeleteObject(ctx, k8sClient, repositoryObj)
		})

		return component
	}

	Context("without a service account", func() {
		BeforeEach(func(ctx SpecContext) {
			createServiceAccount(ctx, namespace, serviceaccount.DefaultName)
			grantOCMConfigReads(ctx, namespace, namespace, serviceaccount.DefaultName)
		})

		It("authorizes a Secret in its own namespace as the default service account", func(ctx SpecContext) {
			component := newComponent(ctx, func(c *v1alpha1.Component) {
				c.Spec.OCMConfig = []v1alpha1.OCMConfiguration{test.SecretOCMConfiguration(local)}
			})

			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{"Status.Component.Version": Version1})
		})

		It("denies a Secret in another namespace until default may get it", func(ctx SpecContext) {
			component := newComponent(ctx, func(c *v1alpha1.Component) {
				c.Spec.OCMConfig = []v1alpha1.OCMConfiguration{test.SecretOCMConfiguration(foreign)}
			})

			test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, component, v1alpha1.GetConfigurationFailedReason, forbiddenForeign)

			grantOCMConfigReads(ctx, other, namespace, serviceaccount.DefaultName)
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{"Status.Component.Version": Version1})
		})

		It("denies config a Repository propagates from another namespace", func(ctx SpecContext) {
			repositoryObj.Status.EffectiveOCMConfig = []v1alpha1.OCMConfiguration{test.SecretOCMConfiguration(foreign)}
			Expect(k8sClient.Status().Update(ctx, repositoryObj)).To(Succeed())

			component := newComponent(ctx, func(*v1alpha1.Component) {})

			test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, component, v1alpha1.GetConfigurationFailedReason, forbiddenForeign)
		})

		It("denies a Repository in another namespace before reading it", func(ctx SpecContext) {
			component := newComponent(ctx, func(c *v1alpha1.Component) {
				c.Spec.OCMConfig = []v1alpha1.OCMConfiguration{{
					NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
						APIVersion: v1alpha1.GroupVersion.String(),
						Kind:       v1alpha1.KindRepository,
						Name:       "team-b-repository",
						Namespace:  other,
					},
					Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
				}}
			})

			test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, component, v1alpha1.GetConfigurationFailedReason,
				`repositories.delivery.ocm.software "team-b-repository" is forbidden`)
		})
	})

	Context("with a service account", func() {
		It("denies a Secret in another namespace until it may get it", func(ctx SpecContext) {
			createServiceAccount(ctx, namespace, reader)
			component := newComponent(ctx, func(c *v1alpha1.Component) {
				c.Spec.ServiceAccountName = reader
				c.Spec.OCMConfig = []v1alpha1.OCMConfiguration{test.SecretOCMConfiguration(foreign)}
			})

			test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, component, v1alpha1.GetConfigurationFailedReason, forbiddenForeign)

			grantOCMConfigReads(ctx, other, namespace, reader)
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{"Status.Component.Version": Version1})
		})

		It("denies a Secret in its own namespace it may not get", func(ctx SpecContext) {
			createServiceAccount(ctx, namespace, reader)
			component := newComponent(ctx, func(c *v1alpha1.Component) {
				c.Spec.ServiceAccountName = reader
				c.Spec.OCMConfig = []v1alpha1.OCMConfiguration{test.SecretOCMConfiguration(local)}
			})

			test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, component, v1alpha1.GetConfigurationFailedReason,
				fmt.Sprintf("service account %s/%s may not get it", namespace, reader))
		})

		It("fails while the service account does not exist", func(ctx SpecContext) {
			component := newComponent(ctx, func(c *v1alpha1.Component) {
				c.Spec.ServiceAccountName = "missing"
			})

			test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, component, v1alpha1.GetConfigurationFailedReason,
				`serviceaccounts "missing" not found`)
		})
	})
})

// createConfigSecret creates the Secret "creds" holding an OCM config in namespace.
func createConfigSecret(ctx context.Context, namespace string) *corev1.Secret {
	GinkgoHelper()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: namespace},
		StringData: map[string]string{v1alpha1.OCMConfigKey: `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: MavenRepository
      hostname: example.com
    credentials:
    - type: Credentials
      properties:
        username: user
        password: secret
`},
	}
	Expect(k8sClient.Create(ctx, secret)).To(Succeed())

	return secret
}

func createServiceAccount(ctx context.Context, namespace, name string) {
	GinkgoHelper()

	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	Expect(k8sClient.Create(ctx, sa)).To(Succeed())
}

// grantOCMConfigReads lets the service account sa of saNamespace get the Secrets, ConfigMaps and OCM objects in
// namespace.
func grantOCMConfigReads(ctx context.Context, namespace, saNamespace, sa string) {
	GinkgoHelper()

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: saNamespace + "-" + sa, Namespace: namespace},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"secrets", "configmaps"}, Verbs: []string{"get"}},
			{
				APIGroups: []string{v1alpha1.GroupVersion.Group},
				Resources: []string{"repositories", "components", "resources"},
				Verbs:     []string{"get"},
			},
		},
	}
	Expect(k8sClient.Create(ctx, role)).To(Succeed())

	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: role.GetName(), Namespace: namespace},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.GetName()},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: sa, Namespace: saNamespace}},
	}
	Expect(k8sClient.Create(ctx, binding)).To(Succeed())
}
