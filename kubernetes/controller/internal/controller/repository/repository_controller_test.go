package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/test"
)

const (
	TestNamespaceOCMRepo = "test-namespace-repository"
	TestRepositoryObj    = "test-repository"
)

var _ = Describe("Repository Controller", func() {
	var (
		namespace *corev1.Namespace
		ocmRepo   *v1alpha1.Repository
	)

	BeforeEach(func(ctx SpecContext) {
		if namespace == nil {
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: TestNamespaceOCMRepo,
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		}
	})

	AfterEach(func(ctx SpecContext) {
		test.DeleteObject(ctx, k8sClient, ocmRepo)
	})

	Describe("Reconciling with different RepositorySpec specifications", func() {

		Context("When correct RepositorySpec is provided", func() {
			It("Repository can be reconciled", func(ctx SpecContext) {
				user, password := getUserAndPasswordForTest()
				if user == "" || password == "" {
					Skip("GitHub credentials not available (gh CLI not found or not authenticated)")
				}

				By("creating a ConfigMap with OCIRepository credentials")
				cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRepository
      hostname: ghcr.io
      port: "443"
      scheme: https
    credentials:
    - type: Credentials/v1
      properties:
        username: %q
        password: %q
`, user, password)

				ocmConfig := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: TestNamespaceOCMRepo,
						Name:      "ghcr-credentials",
					},
					Data: map[string]string{
						v1alpha1.OCMConfigKey: cfg,
					},
				}
				Expect(k8sClient.Create(ctx, ocmConfig)).To(Succeed())
				DeferCleanup(func(ctx SpecContext) {
					Expect(k8sClient.Delete(ctx, ocmConfig)).To(Succeed())
				})

				By("creating a OCI repository with existing host")
				spec := &oci.Repository{
					Type:    runtime.NewVersionedType(oci.Type, "v1"),
					BaseUrl: "ghcr.io",
					SubPath: "open-component-model",
				}
				specdata, err := json.Marshal(spec)
				Expect(err).NotTo(HaveOccurred())
				repoName := TestRepositoryObj + "-passing"
				ocmRepo = newTestRepository(TestNamespaceOCMRepo, repoName, &specdata)

				By("adding ConfigMap reference to Repository")
				ocmRepo.Spec.OCMConfig = []v1alpha1.OCMConfiguration{
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "ConfigMap",
							Name:       ocmConfig.Name,
						},
					},
				}

				Expect(k8sClient.Create(ctx, ocmRepo)).To(Succeed())

				By("check that repository status has been updated successfully")
				Eventually(komega.Object(ocmRepo), "1m").Should(And(
					HaveField("Status.Conditions", ContainElement(
						And(HaveField("Type", Equal(meta.ReadyCondition)), HaveField("Status", Equal(metav1.ConditionTrue))),
					)),
				))
			})
		})

		Context("When incorrect RepositorySpec is provided", func() {
			It("Validation must fail", func(ctx SpecContext) {

				By("creating a OCI repository with non-existing host")
				spec := &oci.Repository{
					Type:    runtime.NewVersionedType(oci.Type, "v1"),
					BaseUrl: "https://doesnotexist",
				}
				specdata, err := json.Marshal(spec)
				Expect(err).NotTo(HaveOccurred())
				repoName := TestRepositoryObj + "-no-host"
				ocmRepo = newTestRepository(TestNamespaceOCMRepo, repoName, &specdata)
				Expect(k8sClient.Create(ctx, ocmRepo)).To(Succeed())

				By("check that repository status has NOT been updated successfully")
				Eventually(komega.Object(ocmRepo), "1m").Should(And(
					HaveField("Status.Conditions", ContainElement(
						And(HaveField("Type", Equal(meta.ReadyCondition)), HaveField("Status", Equal(metav1.ConditionFalse))),
					)),
				))
			})
		})

		Context("When incorrect RepositorySpec is provided", func() {
			It("Validation must fail", func(ctx SpecContext) {

				By("creating a OCI repository from invalid json")
				specdata := []byte("not a json")
				repoName := TestRepositoryObj + "-invalid-json"
				ocmRepo = newTestRepository(TestNamespaceOCMRepo, repoName, &specdata)
				Expect(k8sClient.Create(ctx, ocmRepo)).NotTo(Succeed())
			})
		})

		Context("When incorrect RepositorySpec is provided", func() {
			It("Validation must fail", func(ctx SpecContext) {

				By("creating a OCI repository from a valid json but invalid RepositorySpec")
				specdata := []byte(`{"json":"not a valid RepositorySpec"}`)
				repoName := TestRepositoryObj + "-invalid-spec"
				ocmRepo = newTestRepository(TestNamespaceOCMRepo, repoName, &specdata)
				Expect(k8sClient.Create(ctx, ocmRepo)).To(Succeed())

				By("check that repository status has NOT been updated successfully")
				Eventually(komega.Object(ocmRepo), "1m").Should(And(
					HaveField("Status.Conditions", ContainElement(
						And(HaveField("Type", Equal(meta.ReadyCondition)), HaveField("Status", Equal(metav1.ConditionFalse))),
					)),
				))
			})
		})
	})

	Describe("Reconciling a valid Repository", func() {

		Context("When ConfigRefs properly set", func() {
			It("Repository can be reconciled", func(ctx SpecContext) {

				By("creating secret and config objects")
				configs, secrets := createTestConfigsAndSecrets(ctx)

				By("creating a OCI repository")
				spec := &oci.Repository{
					Type:    runtime.NewVersionedType(oci.Type, "v1"),
					BaseUrl: "ghcr.io",
					SubPath: "open-component-model",
				}
				specdata, err := json.Marshal(spec)
				Expect(err).NotTo(HaveOccurred())
				repoName := TestRepositoryObj + "-all-fields"
				ocmRepo = newTestRepository(TestNamespaceOCMRepo, repoName, &specdata)

				By("adding config and secret refs")
				ocmRepo.Spec.OCMConfig = append(ocmRepo.Spec.OCMConfig, []v1alpha1.OCMConfiguration{
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       secrets[0].Name,
							Namespace:  secrets[0].Namespace,
						},
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       secrets[1].Name,
						},
						Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: secrets[2].Name,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "ConfigMap",
							Name:       configs[0].Name,
							Namespace:  configs[0].Namespace,
						},
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "ConfigMap",
							Name:       configs[1].Name,
						},
						Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							Kind: "ConfigMap",
							Name: configs[2].Name,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
				}...)

				By("creating Repository object")
				Expect(k8sClient.Create(ctx, ocmRepo)).To(Succeed())

				By("check that the ConfigRefs are in the status")
				expectedEffectiveOCMConfig := []v1alpha1.OCMConfiguration{
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       secrets[0].Name,
							Namespace:  secrets[0].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       secrets[1].Name,
							Namespace:  secrets[1].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       secrets[2].Name,
							Namespace:  secrets[2].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "ConfigMap",
							Name:       configs[0].Name,
							Namespace:  configs[0].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "ConfigMap",
							Name:       configs[1].Name,
							Namespace:  configs[1].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "ConfigMap",
							Name:       configs[2].Name,
							Namespace:  configs[2].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
				}
				Eventually(komega.Object(ocmRepo), "15s").Should(And(
					HaveField("Status.Conditions", ContainElement(
						And(HaveField("Type", Equal(meta.ReadyCondition)), HaveField("Status", Equal(metav1.ConditionTrue))),
					)),
					HaveField("Status.EffectiveOCMConfig", ConsistOf(expectedEffectiveOCMConfig)),
					HaveField("GetEffectiveOCMConfig()", ConsistOf(expectedEffectiveOCMConfig)),
				))

				By("cleanup secret and config objects")
				cleanupTestConfigsAndSecrets(ctx, configs, secrets)
			})
		})

		Context("repository controller", func() {
			It("reconciles a repository", func(ctx SpecContext) {
				By("creating a repository object")
				ctfpath := GinkgoT().TempDir()
				componentName := "ocm.software/test-component"

				spec := &ctf.Repository{
					Type:       runtime.NewVersionedType(ctf.Type, "v1"),
					FilePath:   ctfpath,
					AccessMode: ctf.AccessModeReadWrite,
				}
				specdata, err := json.Marshal(spec)
				Expect(err).NotTo(HaveOccurred())
				ocmRepoName := TestRepositoryObj + "-deleted"
				ocmRepo = newTestRepository(TestNamespaceOCMRepo, ocmRepoName, &specdata)

				Expect(k8sClient.Create(ctx, ocmRepo)).To(Succeed())

				By("checking if the repository is ready")
				Eventually(func() bool {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TestNamespaceOCMRepo, Name: ocmRepoName}, ocmRepo)).To(Succeed())
					return conditions.IsReady(ocmRepo)
				}).WithTimeout(5 * time.Second).Should(BeTrue())

				By("creating a component that uses this repository")
				component := &v1alpha1.Component{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: TestNamespaceOCMRepo,
						Name:      "test-component-name",
					},
					Spec: v1alpha1.ComponentSpec{
						RepositoryRef: corev1.LocalObjectReference{
							Name: ocmRepoName,
						},
						Component: componentName,
						Semver:    "1.0.0",
						Interval:  metav1.Duration{Duration: time.Minute * 10},
					},
					Status: v1alpha1.ComponentStatus{},
				}
				Expect(k8sClient.Create(ctx, component)).To(Succeed())

				By("wait for the cache to catch up and create the index field on the component")
				time.Sleep(time.Second)

				By("deleting the repository should not allow the deletion unless the component is removed")
				Expect(k8sClient.Delete(ctx, ocmRepo)).To(Succeed())
				Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TestNamespaceOCMRepo, Name: ocmRepoName}, ocmRepo)).To(Succeed())

				By("removing the component")
				Expect(k8sClient.Delete(ctx, component)).To(Succeed())

				By("checking if the repository is eventually deleted")
				Eventually(func() error {
					err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TestNamespaceOCMRepo, Name: ocmRepoName}, ocmRepo)
					if errors.IsNotFound(err) {
						return nil
					}

					return err
				}).WithTimeout(10 * time.Second).Should(Succeed())
			})
		})
	})
})

func newTestRepository(ns, name string, specdata *[]byte) *v1alpha1.Repository {
	return &v1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Spec: v1alpha1.RepositorySpec{
			RepositorySpec: &apiextensionsv1.JSON{
				Raw: *specdata,
			},
			Interval: metav1.Duration{Duration: time.Minute * 10},
		},
	}
}

func createTestConfigsAndSecrets(ctx context.Context) (configs []*corev1.ConfigMap, secrets []*corev1.Secret) {
	const (
		Config1 = "config1"
		Config2 = "config2"
		Config3 = "config3"

		Secret1 = "secret1"
		Secret2 = "secret2"
		Secret3 = "secret3"
	)

	By("setup configs")
	config1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: TestNamespaceOCMRepo,
			Name:      Config1,
		},
		Data: map[string]string{
			v1alpha1.OCMConfigKey: `
type: generic.config.ocm.software/v1
sets:
  set1:
    description: set1
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: MavenRepository
          hostname: example.com
          pathprefix: path/ocm
        credentials:
        - type: Credentials
          properties:
            username: testuser1
            password: testpassword1 
`,
		},
	}
	configs = append(configs, config1)
	Expect(k8sClient.Create(ctx, config1)).To(Succeed())

	config2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: TestNamespaceOCMRepo,
			Name:      Config2,
		},
		Data: map[string]string{
			v1alpha1.OCMConfigKey: `
type: generic.config.ocm.software/v1
sets:
  set2:
    description: set2
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: MavenRepository
          hostname: example.com
          pathprefix: path/ocm
        credentials:
        - type: Credentials
          properties:
            username: testuser1
            password: testpassword1 
`,
		},
	}
	configs = append(configs, config2)
	Expect(k8sClient.Create(ctx, config2)).To(Succeed())

	config3 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: TestNamespaceOCMRepo,
			Name:      Config3,
		},
		Data: map[string]string{
			v1alpha1.OCMConfigKey: `
type: generic.config.ocm.software/v1
sets:
  set3:
    description: set3
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: MavenRepository
          hostname: example.com
          pathprefix: path/ocm
        credentials:
        - type: Credentials
          properties:
            username: testuser1
            password: testpassword1 
`,
		},
	}
	configs = append(configs, config3)
	Expect(k8sClient.Create(ctx, config3)).To(Succeed())

	By("setup secrets")
	secret1 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: TestNamespaceOCMRepo,
			Name:      Secret1,
		},
		Data: map[string][]byte{
			v1alpha1.OCMConfigKey: []byte(`
type: credentials.config.ocm.software
consumers:
- identity:
    type: MavenRepository
    hostname: example.com
    pathprefix: path1
  credentials:
  - type: Credentials
    properties:
      username: testuser1
      password: testpassword1
`),
		},
	}
	secrets = append(secrets, secret1)
	Expect(k8sClient.Create(ctx, secret1)).To(Succeed())

	secret2 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: TestNamespaceOCMRepo,
			Name:      Secret2,
		},
		Data: map[string][]byte{
			v1alpha1.OCMConfigKey: []byte(`
type: credentials.config.ocm.software
consumers:
- identity:
    type: MavenRepository
    hostname: example.com
    pathprefix: path2
  credentials:
  - type: Credentials
    properties:
      username: testuser2
      password: testpassword2
`),
		},
	}
	secrets = append(secrets, secret2)
	Expect(k8sClient.Create(ctx, secret2)).To(Succeed())

	secret3 := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: TestNamespaceOCMRepo,
			Name:      Secret3,
		},
		Data: map[string][]byte{
			v1alpha1.OCMConfigKey: []byte(`
type: credentials.config.ocm.software
consumers:
- identity:
    type: MavenRepository
    hostname: example.com
    pathprefix: path3
  credentials:
  - type: Credentials
    properties:
      username: testuser3
      password: testpassword3
`),
		},
	}
	secrets = append(secrets, &secret3)
	Expect(k8sClient.Create(ctx, &secret3)).To(Succeed())

	return configs, secrets
}

func cleanupTestConfigsAndSecrets(ctx context.Context, configs []*corev1.ConfigMap, secrets []*corev1.Secret) {
	for _, config := range configs {
		Expect(k8sClient.Delete(ctx, config)).To(Succeed())
	}
	for _, secret := range secrets {
		Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
	}
}

// getUserAndPasswordForTest safely gets GitHub credentials for testing
func getUserAndPasswordForTest() (string, string) {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return "", ""
	}

	user, err := getUsername(gh)
	if err != nil {
		return "", ""
	}

	pw := exec.Command("sh", "-c", fmt.Sprintf("%s auth token", gh))
	out, err := pw.CombinedOutput()
	if err != nil {
		return "", ""
	}
	password := strings.TrimSpace(string(out))

	return user, password
}

func getUsername(gh string) (string, error) {
	if githubUser := os.Getenv("GITHUB_USER"); githubUser != "" {
		return githubUser, nil
	}

	out, err := exec.Command("sh", "-c", fmt.Sprintf("%s api user", gh)).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh CLI for user failed: %w", err)
	}
	structured := map[string]interface{}{}
	if err := json.Unmarshal(out, &structured); err != nil {
		return "", fmt.Errorf("gh failed to parse output: %w", err)
	}

	return structured["login"].(string), nil
}
