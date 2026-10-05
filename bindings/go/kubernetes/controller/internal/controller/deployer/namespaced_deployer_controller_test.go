package deployer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/applyset"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const namespacedDeployerSA = "deployer-sa"

var _ = Describe("NamespacedDeployer Controller", func() {
	var namespace *corev1.Namespace

	BeforeEach(func(ctx SpecContext) {
		namespace = test.NamespaceForTest(ctx)
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
	})

	It("applies into its own namespace as the service account and prunes on deletion", func(ctx SpecContext) {
		createServiceAccount(ctx, namespace.GetName(), true)
		resourceObj := mockYAMLResource(ctx, namespace.GetName(), `apiVersion: v1
kind: ConfigMap
metadata:
  name: nd-cm
data:
  hello: world
`)

		deployer := createNamespacedDeployer(ctx, namespace.GetName(), resourceObj.GetName(), nil)
		test.WaitForReadyObject(ctx, k8sClient, deployer, map[string]any{})

		By("verifying the ConfigMap was defaulted into the deployer's namespace and is owned by it")
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.GetName(), Name: "nd-cm"}, cm)).To(Succeed())
		Expect(cm.Data).To(HaveKeyWithValue("hello", "world"))
		owner := metav1.GetControllerOf(cm)
		Expect(owner).NotTo(BeNil())
		Expect(owner.Kind).To(Equal(v1alpha1.KindNamespacedDeployer))
		Expect(owner.UID).To(Equal(deployer.GetUID()))
		Expect(owner.BlockOwnerDeletion).To(HaveValue(BeFalse()))
		deployer.SetGroupVersionKind(v1alpha1.GroupVersion.WithKind(v1alpha1.KindNamespacedDeployer))
		Expect(cm.GetLabels()).To(HaveKeyWithValue(applyset.ApplysetPartOfLabel, applyset.ID(deployer)))
		Expect(deployer.Status.Deployed).To(ContainElement(HaveField("Name", "nd-cm")))

		By("deleting the NamespacedDeployer prunes the ConfigMap")
		test.DeleteObject(ctx, k8sClient, deployer)
		Eventually(func(ctx context.Context) bool {
			return errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), &corev1.ConfigMap{}))
		}).WithTimeout(test.DefaultKubernetesOperationTimeout).WithContext(ctx).Should(BeTrue())
	})

	It("does not apply when the service account lacks permissions", func(ctx SpecContext) {
		createServiceAccount(ctx, namespace.GetName(), false)
		resourceObj := mockYAMLResource(ctx, namespace.GetName(), `apiVersion: v1
kind: ConfigMap
metadata:
  name: nd-forbidden-cm
`)

		deployer := createNamespacedDeployer(ctx, namespace.GetName(), resourceObj.GetName(), nil)
		waitForNotReadyMessage(ctx, deployer, v1alpha1.ApplyFailed, "forbidden")

		err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.GetName(), Name: "nd-forbidden-cm"}, &corev1.ConfigMap{})
		Expect(errors.IsNotFound(err)).To(BeTrue())
	})

	It("does not apply when the service account does not exist", func(ctx SpecContext) {
		resourceObj := mockYAMLResource(ctx, namespace.GetName(), `apiVersion: v1
kind: ConfigMap
metadata:
  name: nd-missing-sa-cm
`)

		deployer := createNamespacedDeployer(ctx, namespace.GetName(), resourceObj.GetName(), nil)
		waitForNotReadyMessage(ctx, deployer, v1alpha1.ApplyFailed, namespacedDeployerSA)
	})

	It("applies cluster-scoped and cross-namespace objects", func(ctx SpecContext) {
		createServiceAccount(ctx, namespace.GetName(), true)
		other := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace.GetName() + "-other"}}
		Expect(k8sClient.Create(ctx, other)).To(Succeed())
		grantConfigMaps(ctx, other.GetName(), namespace.GetName())
		grantClusterRoles(ctx, namespace.GetName())

		clusterRoleName := "nd-" + namespace.GetName()
		resourceObj := mockYAMLResource(ctx, namespace.GetName(), fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: nd-cross-namespace-cm
  namespace: %s
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: %s
`, other.GetName(), clusterRoleName))

		deployer := createNamespacedDeployer(ctx, namespace.GetName(), resourceObj.GetName(), nil)
		test.WaitForReadyObject(ctx, k8sClient, deployer, map[string]any{})

		By("verifying both objects carry the deployer annotation but no owner reference")
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: other.GetName(), Name: "nd-cross-namespace-cm"}, cm)).To(Succeed())
		clusterRole := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: clusterRoleName}, clusterRole)).To(Succeed())
		for _, obj := range []client.Object{cm, clusterRole} {
			Expect(obj.GetOwnerReferences()).To(BeEmpty())
			Expect(obj.GetAnnotations()).To(HaveKeyWithValue(namespacedDeployerAnnotation, client.ObjectKeyFromObject(deployer).String()))
		}

		By("re-applying the cross-namespace object after it was deleted")
		oldUID := cm.GetUID()
		Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
		Eventually(func(g Gomega, ctx context.Context) {
			reapplied := &corev1.ConfigMap{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), reapplied)).To(Succeed())
			g.Expect(reapplied.GetUID()).NotTo(Equal(oldUID))
		}).WithTimeout(test.DefaultKubernetesOperationTimeout).WithContext(ctx).Should(Succeed())

		By("deleting the NamespacedDeployer prunes both objects")
		test.DeleteObject(ctx, k8sClient, deployer)
		Eventually(func(ctx context.Context) bool {
			return errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), &corev1.ConfigMap{})) &&
				errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(clusterRole), &rbacv1.ClusterRole{}))
		}).WithTimeout(test.DefaultKubernetesOperationTimeout).WithContext(ctx).Should(BeTrue())
	})

	It("orphans cluster-scoped objects when its RBAC is deleted first", func(ctx SpecContext) {
		createServiceAccount(ctx, namespace.GetName(), true)
		grantClusterRoles(ctx, namespace.GetName())

		clusterRoleName := "nd-orphan-" + namespace.GetName()
		resourceObj := mockYAMLResource(ctx, namespace.GetName(), fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: %s
`, clusterRoleName))

		deployer := createNamespacedDeployer(ctx, namespace.GetName(), resourceObj.GetName(), nil)
		test.WaitForReadyObject(ctx, k8sClient, deployer, map[string]any{})
		clusterRole := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName}}
		DeferCleanup(func(ctx SpecContext) {
			test.DeleteObject(ctx, k8sClient, clusterRole)
		})

		By("deleting the service account's binding and the service account like `kubectl delete -f` would")
		test.DeleteObject(ctx, k8sClient, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "nd-deployer-" + namespace.GetName()}})
		test.DeleteObject(ctx, k8sClient, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: namespacedDeployerSA, Namespace: namespace.GetName()}})

		By("deleting the NamespacedDeployer completes and leaves the ClusterRole behind")
		test.DeleteObject(ctx, k8sClient, deployer)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(clusterRole), &rbacv1.ClusterRole{})).To(Succeed())
	})

	It("does not apply into another namespace without permissions there", func(ctx SpecContext) {
		createServiceAccount(ctx, namespace.GetName(), true)
		resourceObj := mockYAMLResource(ctx, namespace.GetName(), `apiVersion: v1
kind: ConfigMap
metadata:
  name: nd-foreign-cm
  namespace: default
`)

		deployer := createNamespacedDeployer(ctx, namespace.GetName(), resourceObj.GetName(), nil)
		waitForNotReadyMessage(ctx, deployer, v1alpha1.ApplyFailed, "forbidden")

		err := k8sClient.Get(ctx, client.ObjectKey{Namespace: metav1.NamespaceDefault, Name: "nd-foreign-cm"}, &corev1.ConfigMap{})
		Expect(errors.IsNotFound(err)).To(BeTrue())
	})

	It("rejects ocm configuration from another namespace", func(ctx SpecContext) {
		createServiceAccount(ctx, namespace.GetName(), true)
		resourceObj := mockYAMLResource(ctx, namespace.GetName(), `apiVersion: v1
kind: ConfigMap
metadata:
  name: nd-config-cm
`)

		deployer := createNamespacedDeployer(ctx, namespace.GetName(), resourceObj.GetName(), []v1alpha1.OCMConfiguration{{
			NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
				APIVersion: corev1.SchemeGroupVersion.String(),
				Kind:       "Secret",
				Name:       "foreign-credentials",
				Namespace:  metav1.NamespaceDefault,
			},
			Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
		}})
		waitForNotReadyMessage(ctx, deployer, v1alpha1.GetConfigurationFailedReason, "outside of namespace")
	})

	It("requires a service account name", func(ctx SpecContext) {
		deployer := &v1alpha1.NamespacedDeployer{
			ObjectMeta: metav1.ObjectMeta{Name: "nd-no-sa", Namespace: namespace.GetName()},
			Spec: v1alpha1.NamespacedDeployerSpec{
				ResourceRef: corev1.LocalObjectReference{Name: "any"},
			},
		}
		Expect(errors.IsInvalid(k8sClient.Create(ctx, deployer))).To(BeTrue())
	})
})

// waitForNotReadyMessage waits for a Ready=False condition with the given reason and message. Matching the reason
// alone is racy, as a transient error such as an update conflict can carry the same reason.
func waitForNotReadyMessage(ctx SpecContext, deployer *v1alpha1.NamespacedDeployer, reason, message string) {
	GinkgoHelper()

	Eventually(func(g Gomega, ctx context.Context) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deployer), deployer)).To(Succeed())
		cond := apimeta.FindStatusCondition(deployer.Status.Conditions, v1alpha1.ReadyCondition)
		g.Expect(cond).NotTo(BeNil())
		g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		g.Expect(cond.Reason).To(Equal(reason))
		g.Expect(cond.Message).To(ContainSubstring(message))
	}).WithTimeout(test.DefaultKubernetesOperationTimeout).WithContext(ctx).Should(Succeed())
}

// createServiceAccount creates the deployer service account and, if bind is set, grants it full access to
// ConfigMaps in the namespace.
func createServiceAccount(ctx SpecContext, namespace string, bind bool) {
	GinkgoHelper()

	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: namespacedDeployerSA, Namespace: namespace}}
	Expect(k8sClient.Create(ctx, sa)).To(Succeed())
	if !bind {
		return
	}

	grantConfigMaps(ctx, namespace, namespace)
}

// grantConfigMaps grants the deployer service account of saNamespace full access to ConfigMaps in namespace.
func grantConfigMaps(ctx SpecContext, namespace, saNamespace string) {
	GinkgoHelper()

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: namespacedDeployerSA, Namespace: namespace},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
			Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
		}},
	}
	Expect(k8sClient.Create(ctx, role)).To(Succeed())

	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: namespacedDeployerSA, Namespace: namespace},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.GetName()},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: namespacedDeployerSA, Namespace: saNamespace}},
	}
	Expect(k8sClient.Create(ctx, binding)).To(Succeed())
}

// grantClusterRoles grants the deployer service account of saNamespace full access to ClusterRoles.
func grantClusterRoles(ctx SpecContext, saNamespace string) {
	GinkgoHelper()

	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "nd-deployer-" + saNamespace},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{rbacv1.GroupName},
			Resources: []string{"clusterroles"},
			Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
		}},
	}
	Expect(k8sClient.Create(ctx, role)).To(Succeed())

	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: role.GetName()},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: role.GetName()},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: namespacedDeployerSA, Namespace: saNamespace}},
	}
	Expect(k8sClient.Create(ctx, binding)).To(Succeed())
	DeferCleanup(func(ctx SpecContext) {
		test.DeleteObject(ctx, k8sClient, binding)
		test.DeleteObject(ctx, k8sClient, role)
	})
}

func createNamespacedDeployer(ctx SpecContext, namespace, resourceName string, cfg []v1alpha1.OCMConfiguration) *v1alpha1.NamespacedDeployer {
	GinkgoHelper()

	deployer := &v1alpha1.NamespacedDeployer{
		ObjectMeta: metav1.ObjectMeta{Name: "nd", Namespace: namespace},
		Spec: v1alpha1.NamespacedDeployerSpec{
			ResourceRef:        corev1.LocalObjectReference{Name: resourceName},
			ServiceAccountName: namespacedDeployerSA,
			OCMConfig:          cfg,
		},
	}
	Expect(k8sClient.Create(ctx, deployer)).To(Succeed())
	DeferCleanup(func(ctx SpecContext) {
		test.DeleteObject(ctx, k8sClient, deployer)
	})

	return deployer
}

// mockYAMLResource stores the manifest as a local blob in a CTF and mocks a ready Component and Resource for it.
func mockYAMLResource(ctx SpecContext, namespace, manifest string) *v1alpha1.Resource {
	GinkgoHelper()

	componentName := "ocm.software/namespaced-deployer-" + namespace
	componentVersion := "v1.0.0"
	resourceName := "nd-resource"
	tempDir := GinkgoT().TempDir()

	fs, err := filesystem.NewFS(tempDir, os.O_RDWR)
	Expect(err).NotTo(HaveOccurred())
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))), oci.WithTempDir(tempDir))
	Expect(err).NotTo(HaveOccurred())

	res := &descruntime.Resource{
		ElementMeta: descruntime.ElementMeta{
			ObjectMeta: descruntime.ObjectMeta{Name: resourceName, Version: "1.0.0"},
		},
		Type:     "plainText",
		Relation: descruntime.LocalRelation,
		Access: &v2.LocalBlob{
			Type:      runtime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
			MediaType: "application/x-yaml",
		},
	}
	newRes, err := repo.AddLocalResource(ctx, componentName, componentVersion, res, inmemory.New(bytes.NewReader([]byte(manifest))))
	Expect(err).NotTo(HaveOccurred())

	Expect(repo.AddComponentVersion(ctx, &descruntime.Descriptor{
		Meta: descruntime.Meta{Version: "v2"},
		Component: descruntime.Component{
			ComponentMeta: descruntime.ComponentMeta{
				ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: componentVersion},
			},
			Provider:  descruntime.Provider{Name: "ocm.software"},
			Resources: []descruntime.Resource{*newRes},
		},
	})).To(Succeed())

	specData, err := json.Marshal(&ctfv1.Repository{
		Type:       runtime.Type{Name: "ctf", Version: "v1"},
		FilePath:   tempDir,
		AccessMode: ctfv1.AccessModeReadOnly,
	})
	Expect(err).NotTo(HaveOccurred())

	info := v1alpha1.ComponentInfo{
		Component:      componentName,
		Version:        componentVersion,
		RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
	}
	componentObj := test.MockComponent(ctx, "nd-component", namespace, &test.MockComponentOptions{
		Client:   k8sClient,
		Recorder: recorder,
		Info:     info,
	})
	DeferCleanup(func(ctx SpecContext) {
		test.DeleteObject(ctx, k8sClient, componentObj)
	})

	resourceObj := test.MockResource(ctx, resourceName, namespace, &test.MockResourceOptions{
		ComponentRef:  corev1.LocalObjectReference{Name: componentObj.GetName()},
		Clnt:          k8sClient,
		Recorder:      recorder,
		ComponentInfo: &info,
		ResourceInfo: &v1alpha1.ResourceInfo{
			Name:    resourceName,
			Type:    "plainText",
			Version: "1.0.0",
			Access:  apiextensionsv1.JSON{Raw: []byte(`{"type":"localBlob/v1"}`)},
			Digest: &v2.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "genericBlobDigest/v1",
				// unique per namespace so the shared download cache never serves another spec's manifest
				Value: fmt.Sprintf("nd-digest-%s", namespace),
			},
		},
	})
	DeferCleanup(func(ctx SpecContext) {
		test.DeleteObject(ctx, k8sClient, resourceObj)
	})

	return resourceObj
}
