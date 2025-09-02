package replication

import (
	"context"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/mandelsoft/vfs/pkg/osfs"
	"k8s.io/apimachinery/pkg/types"
	"ocm.software/ocm/api/ocm/extensions/accessmethods/ociartifact"
	"ocm.software/ocm/api/ocm/extensions/repositories/ctf"
	"ocm.software/ocm/api/ocm/extensions/repositories/genericocireg"
	"ocm.software/ocm/api/utils/accessio"

	mandelsoft "github.com/mandelsoft/goutils/testutils"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ocmbuilder "ocm.software/ocm/api/helper/builder"
	environment "ocm.software/ocm/api/helper/env"
	ocmmetav1 "ocm.software/ocm/api/ocm/compdesc/meta/v1"
	resourcetypes "ocm.software/ocm/api/ocm/extensions/artifacttypes"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

const reconciliationInterval = time.Second * 3

const OCMConfigResourcesByValue = `
type: generic.config.ocm.software/v1
configurations:
  - type: transport.ocm.config.ocm.software
    recursive: true
    overwrite: true
    localResourcesByValue: false
    resourcesByValue: true
    sourcesByValue: false
    keepGlobalAccess: false
    stopOnExistingVersion: false
`

var _ = Describe("Replication Controller", func() {
	Context("when transferring component versions (CTFs)", func() {
		const (
			testNamespace = "replication-controller-test"
			compOCMName   = "ocm.software/component-for-replication"
			compVersion   = "0.1.0"

			// "gcr.io/google_containers/echoserver:1.10" is taken as test content, because it is used to explain OCM here:
			// https://ocm.software/docs/getting-started/getting-started-with-ocm/create-a-component-version/#add-an-image-reference-option-1
			testImage      = "gcr.io/google_containers/echoserver:1.10"
			localImagePath = "blobs/sha256.4b93359cc643b5d8575d4f96c2d107b4512675dcfee1fa035d0c44a00b9c027c"
		)

		var (
			namespace *corev1.Namespace
			env       *ocmbuilder.Builder
		)

		var (
			replResourceName       string
			compResourceName       string
			optResourceName        string
			sourceRepoResourceName string
			targetRepoResourceName string
		)

		var replNamespacedName types.NamespacedName

		var iteration = 0

		BeforeEach(func(ctx SpecContext) {
			env = ocmbuilder.NewBuilder(environment.FileSystem(osfs.OsFs))
			DeferCleanup(env.Cleanup)

			if namespace == nil {
				namespace = &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: testNamespace,
					},
				}
				Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			}

			iteration++
			i := strconv.Itoa(iteration)
			replResourceName = "test-replication" + i
			compResourceName = "test-component" + i
			optResourceName = "test-transfer-options" + i
			sourceRepoResourceName = "test-source-repository" + i
			targetRepoResourceName = "test-target-repository" + i

			replNamespacedName = types.NamespacedName{
				Name:      replResourceName,
				Namespace: testNamespace,
			}
		})

		It("should be properly reflected in the history", func(ctx SpecContext) {
			By("Create source CTF")
			sourcePath := GinkgoT().TempDir()

			newTestComponentVersionInCTFDir(env, sourcePath, compOCMName, compVersion, testImage)

			By("Create source repository resource")
			sourceRepo, sourceSpecData := newTestCFTRepository(testNamespace, sourceRepoResourceName, sourcePath)
			Expect(k8sClient.Create(ctx, sourceRepo)).To(Succeed())

			By("Simulate repository controller for source repository")
			conditions.MarkTrue(sourceRepo, meta.ReadyCondition, "ready", "")
			Expect(k8sClient.Status().Update(ctx, sourceRepo)).To(Succeed())

			By("Create source component resource")
			component := newTestComponent(testNamespace, compResourceName, sourceRepoResourceName, compOCMName, compVersion)
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("Simulate component controller")
			component.Status.Component = *newTestComponentInfo(compOCMName, compVersion, sourceSpecData)
			conditions.MarkTrue(component, meta.ReadyCondition, "ready", "")
			ocmContextCache.Clear()
			Expect(k8sClient.Status().Update(ctx, component)).To(Succeed())

			By("Create target CTF")
			targetPath := GinkgoT().TempDir()

			By("Create target repository resource")
			targetRepo, targetSpecData := newTestCFTRepository(testNamespace, targetRepoResourceName, targetPath)
			Expect(k8sClient.Create(ctx, targetRepo)).To(Succeed())

			By("Simulate repository controller for target repository")
			targetRepo.Spec.RepositorySpec.Raw = *targetSpecData
			conditions.MarkTrue(targetRepo, meta.ReadyCondition, "ready", "")
			ocmContextCache.Clear()
			Expect(k8sClient.Status().Update(ctx, targetRepo)).To(Succeed())

			By("Create and reconcile Replication resource")
			replication := newTestReplication(testNamespace, replResourceName, compResourceName, targetRepoResourceName)
			Expect(k8sClient.Create(ctx, replication)).To(Succeed())

			Eventually(func(g Gomega, ctx context.Context) {
				replication = &v1alpha1.Replication{}
				g.Expect(k8sClient.Get(ctx, replNamespacedName, replication)).To(Succeed())
				g.Expect(replication.Status).ToNot(BeNil())
				g.Expect(replication.Status.Conditions).ToNot(BeNil())
				g.Expect(conditions.IsReady(replication)).To(BeTrue())
				g.Expect(replication.Status.ObservedGeneration).To(BeNumerically(">", 0))
			}).WithContext(ctx).Should(Succeed())

			Expect(replication.Status.History).To(HaveLen(1))
			Expect(replication.Status.History[0].Component).To(Equal(compOCMName))
			Expect(replication.Status.History[0].Version).To(Equal(compVersion))
			Expect(replication.Status.History[0].SourceRepositorySpec).To(Equal(string(*sourceSpecData)))
			Expect(replication.Status.History[0].TargetRepositorySpec).To(Equal(string(*targetSpecData)))
			Expect(replication.Status.History[0].StartTime).NotTo(BeZero())
			Expect(replication.Status.History[0].EndTime).NotTo(BeZero())
			Expect(replication.Status.History[0].Error).To(BeEmpty())
			Expect(replication.Status.History[0].Success).To(BeTrue())

			By("Create a newer component version")
			compNewVersion := "0.2.0"
			newTestComponentVersionInCTFDir(env, sourcePath, compOCMName, compNewVersion, testImage)

			By("Simulate component controller discovering the newer version")
			component.Status.Component = *newTestComponentInfo(compOCMName, compNewVersion, sourceSpecData)
			conditions.MarkTrue(component, meta.ReadyCondition, "ready", "")
			ocmContextCache.Clear()
			Expect(k8sClient.Status().Update(ctx, component)).To(Succeed())

			By("Expect Replication controller to transfer the new version within the interval")
			replication = &v1alpha1.Replication{}
			Eventually(func(g Gomega, ctx context.Context) {
				g.Expect(k8sClient.Get(ctx, replNamespacedName, replication)).To(Succeed())
				// Wait for the second entry in the history
				g.Expect(conditions.IsReady(replication)).To(BeTrue())
				g.Expect(replication.Status.History).To(HaveLen(2))
			}).WithContext(ctx).Should(Succeed())

			// Expect see the new component version in the history
			Expect(replication.Status.History[1].Version).To(Equal(compNewVersion))

			By("Cleanup the resources")
			Expect(k8sClient.Delete(ctx, replication)).To(Succeed())
			Expect(k8sClient.Delete(ctx, targetRepo)).To(Succeed())
			Expect(k8sClient.Delete(ctx, component)).To(Succeed())
			Expect(k8sClient.Delete(ctx, sourceRepo)).To(Succeed())
		})

		It("should be possible to configure transfer options", func(ctx SpecContext) {

			By("Create source CTF")
			sourcePath := GinkgoT().TempDir()

			newTestComponentVersionInCTFDir(env, sourcePath, compOCMName, compVersion, testImage)

			By("Create source repository resource")
			sourceRepo, sourceSpecData := newTestCFTRepository(testNamespace, sourceRepoResourceName, sourcePath)
			Expect(k8sClient.Create(ctx, sourceRepo)).To(Succeed())

			By("Simulate repository controller for source repository")
			conditions.MarkTrue(sourceRepo, meta.ReadyCondition, "ready", "")
			Expect(k8sClient.Status().Update(ctx, sourceRepo)).To(Succeed())

			By("Create source component resource")
			component := newTestComponent(testNamespace, compResourceName, sourceRepoResourceName, compOCMName, compVersion)
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("Simulate component controller")
			component.Status.Component = *newTestComponentInfo(compOCMName, compVersion, sourceSpecData)
			conditions.MarkTrue(component, meta.ReadyCondition, "ready", "")
			ocmContextCache.Clear()
			Expect(k8sClient.Status().Update(ctx, component)).To(Succeed())

			By("Create target CTF")
			targetPath := GinkgoT().TempDir()

			By("Create target repository resource")
			targetRepo, targetSpecData := newTestCFTRepository(testNamespace, targetRepoResourceName, targetPath)
			Expect(k8sClient.Create(ctx, targetRepo)).To(Succeed())

			By("Simulate repository controller for target repository")
			targetRepo.Spec.RepositorySpec.Raw = *targetSpecData
			conditions.MarkTrue(targetRepo, meta.ReadyCondition, "ready", "")
			ocmContextCache.Clear()
			Expect(k8sClient.Status().Update(ctx, targetRepo)).To(Succeed())

			By("Create ConfigMap with transfer options")
			configMap := newTestConfigMapForData(testNamespace, optResourceName, OCMConfigResourcesByValue)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			By("Create and reconcile Replication resource")
			replication := newTestReplication(testNamespace, replResourceName, compResourceName, targetRepoResourceName)
			replication.Spec.OCMConfig = []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						Kind: "ConfigMap",
						Name: optResourceName,
					},
				},
			}
			Expect(k8sClient.Create(ctx, replication)).To(Succeed())

			By("Wait for reconciliation to run")
			replication = &v1alpha1.Replication{}
			Eventually(func(g Gomega, ctx context.Context) {
				g.Expect(k8sClient.Get(ctx, replNamespacedName, replication)).To(Succeed())
				g.Expect(conditions.IsReady(replication)).To(BeTrue())
				g.Expect(replication.Status.History).To(HaveLen(1))
			}).WithContext(ctx).Should(Succeed())

			// Expect to see the transfered component version in the history
			Expect(replication.Status.History[0].Version).To(Equal(compVersion))

			// Check the effect of transfer options.
			// This docker image was downloaded due to 'resourcesByValue: true' set in the transfer options
			imageArtifact := filepath.Join(targetPath, localImagePath)
			Expect(imageArtifact).To(BeAnExistingFile())

			By("Cleanup the resources")
			Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			Expect(k8sClient.Delete(ctx, replication)).To(Succeed())
			Expect(k8sClient.Delete(ctx, targetRepo)).To(Succeed())
			Expect(k8sClient.Delete(ctx, component)).To(Succeed())
			Expect(k8sClient.Delete(ctx, sourceRepo)).To(Succeed())
		})

		It("transfer errors should be properly reflected in the history", func(ctx SpecContext) {
			By("Create source CTF")
			sourcePath := GinkgoT().TempDir()

			// The created directory is empty, i.e. the test will try to transfer a non-existing component version.
			// This should result in an error, logged in the status of the replication object.

			By("Create source repository resource")
			sourceRepo, sourceSpecData := newTestCFTRepository(testNamespace, sourceRepoResourceName, sourcePath)
			Expect(k8sClient.Create(ctx, sourceRepo)).To(Succeed())

			By("Simulate repository controller for source repository")
			conditions.MarkTrue(sourceRepo, meta.ReadyCondition, "ready", "")
			Expect(k8sClient.Status().Update(ctx, sourceRepo)).To(Succeed())

			By("Create source component resource")
			component := newTestComponent(testNamespace, compResourceName, sourceRepoResourceName, compOCMName, compVersion)
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("Simulate component controller")
			component.Status.Component = *newTestComponentInfo(compOCMName, compVersion, sourceSpecData)
			conditions.MarkTrue(component, meta.ReadyCondition, "ready", "")
			ocmContextCache.Clear()
			Expect(k8sClient.Status().Update(ctx, component)).To(Succeed())

			By("Create target CTF")
			targetPath := GinkgoT().TempDir()

			By("Create target repository resource")
			targetRepo, targetSpecData := newTestCFTRepository(testNamespace, targetRepoResourceName, targetPath)
			Expect(k8sClient.Create(ctx, targetRepo)).To(Succeed())

			By("Simulate repository controller for target repository")
			targetRepo.Spec.RepositorySpec.Raw = *targetSpecData
			conditions.MarkTrue(targetRepo, meta.ReadyCondition, "ready", "")
			ocmContextCache.Clear()
			Expect(k8sClient.Status().Update(ctx, targetRepo)).To(Succeed())

			By("Create and reconcile Replication resource")
			replication := newTestReplication(testNamespace, replResourceName, compResourceName, targetRepoResourceName)
			Expect(k8sClient.Create(ctx, replication)).To(Succeed())

			// Wait until there is something in the history
			Eventually(Object(replication), "30s").WithContext(ctx).Should(
				HaveField("Status.History", Not(BeEmpty())))

			// Check that the reconciliation consistently fails (due to physically non-existing component version).
			// The assumption here is that after the first error k8s will apply a backoff strategy that will trigger
			// a couple of more reconciliation attempts
			prevStartTime := replication.Status.History[0].StartTime
			expectedErrorMsg := "cannot lookup component version in source repository: component version \"" + compOCMName + ":" + compVersion + "\" not found"

			Eventually(func(g Gomega, ctx context.Context) bool {
				g.Expect(conditions.IsReady(replication)).To(BeFalse(), "Expect replication to fail")
				g.Expect(len(replication.Status.History)).To(Equal(1), "Expect history to only contain one entry")

				historyEntry := replication.Status.History[0]
				g.Expect(historyEntry.Success).To(BeFalse())
				g.Expect(historyEntry.Error).To(HavePrefix(expectedErrorMsg))

				// If the current StartTime is after the stored StartTime, we know that another Reconciliation was
				// processed
				if historyEntry.StartTime.After(prevStartTime.Time) {
					return true
				}

				// Get an update of the replication object
				replication = &v1alpha1.Replication{}
				g.Expect(k8sClient.Get(ctx, replNamespacedName, replication)).To(Succeed())

				return false
			}).WithContext(ctx).Should(BeTrue())

			// Check that the other fields are properly set.
			Expect(replication.Status.History[0].Component).To(Equal(compOCMName))
			Expect(replication.Status.History[0].Version).To(Equal(compVersion))
			Expect(replication.Status.History[0].SourceRepositorySpec).To(Equal(string(*sourceSpecData)))
			Expect(replication.Status.History[0].TargetRepositorySpec).To(Equal(string(*targetSpecData)))
			Expect(replication.Status.History[0].StartTime).NotTo(BeZero())
			Expect(replication.Status.History[0].EndTime).NotTo(BeZero())

			By("Cleanup the resources")
			Expect(k8sClient.Delete(ctx, replication)).To(Succeed())
			Expect(k8sClient.Delete(ctx, targetRepo)).To(Succeed())
			Expect(k8sClient.Delete(ctx, component)).To(Succeed())
			Expect(k8sClient.Delete(ctx, sourceRepo)).To(Succeed())
		})
	})
})

func newTestComponentVersionInCTFDir(env *ocmbuilder.Builder, path, compName, compVersion, img string) {
	env.OCMCommonTransport(path, accessio.FormatDirectory, func() {
		env.Component(compName, func() {
			env.Version(compVersion, func() {
				env.Resource("image", "1.0.0", resourcetypes.OCI_IMAGE, ocmmetav1.ExternalRelation, func() {
					env.Access(
						ociartifact.New(img),
					)
				})
			})
		})
	})
}

func newTestCFTRepository(namespace, name, path string) (*v1alpha1.Repository, *[]byte) {
	spec := mandelsoft.Must(ctf.NewRepositorySpec(ctf.ACC_CREATE, path))

	return newTestRepository(namespace, name, spec)
}

func newTestRepository(namespace, name string, spec *genericocireg.RepositorySpec) (*v1alpha1.Repository, *[]byte) {
	specData := mandelsoft.Must(spec.MarshalJSON())

	return &v1alpha1.Repository{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Repository",
			APIVersion: v1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: v1alpha1.RepositorySpec{
			RepositorySpec: &apiextensionsv1.JSON{
				Raw: specData,
			},
			Interval: metav1.Duration{Duration: reconciliationInterval},
		},
	}, &specData
}

func newTestComponent(namespace, name, repoName, ocmName, ocmVersion string) *v1alpha1.Component {
	return &v1alpha1.Component{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Component",
			APIVersion: v1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: v1alpha1.ComponentSpec{
			RepositoryRef: corev1.LocalObjectReference{
				Name: repoName,
			},
			Component: ocmName,
			Semver:    ocmVersion,
			Interval:  metav1.Duration{Duration: reconciliationInterval},
		},
	}
}

func newTestComponentInfo(ocmName, ocmVersion string, rawRepoSpec *[]byte) *v1alpha1.ComponentInfo {
	return &v1alpha1.ComponentInfo{
		RepositorySpec: &apiextensionsv1.JSON{Raw: *rawRepoSpec},
		Component:      ocmName,
		Version:        ocmVersion,
	}
}

func newTestReplication(namespace, name, compName, targetRepoName string) *v1alpha1.Replication {
	return &v1alpha1.Replication{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Replication",
			APIVersion: v1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ReplicationSpec{
			ComponentRef: v1alpha1.ObjectKey{
				Name:      compName,
				Namespace: namespace,
			},
			TargetRepositoryRef: v1alpha1.ObjectKey{
				Name:      targetRepoName,
				Namespace: namespace,
			},
			Interval: metav1.Duration{Duration: reconciliationInterval},
		},
	}
}

func newTestConfigMapForData(namespace, name, data string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: map[string]string{
			v1alpha1.OCMConfigKey: data,
		},
	}
}
