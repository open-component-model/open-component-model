package deployer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"github.com/mandelsoft/vfs/pkg/osfs"
	"github.com/mandelsoft/vfs/pkg/projectionfs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	. "ocm.software/ocm/api/helper/builder"
	environment "ocm.software/ocm/api/helper/env"
	ocmmetav1 "ocm.software/ocm/api/ocm/compdesc/meta/v1"
	"ocm.software/ocm/api/ocm/extensions/artifacttypes"
	"ocm.software/ocm/api/ocm/extensions/repositories/ctf"
	"ocm.software/ocm/api/utils/accessio"
	"ocm.software/ocm/api/utils/mime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/test"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Deployer Controller Pruning", func() {
	var (
		env     *Builder
		tempDir string
	)
	BeforeEach(func() {
		tempDir = GinkgoT().TempDir()
		fs, err := projectionfs.New(osfs.OsFs, tempDir)
		Expect(err).NotTo(HaveOccurred())
		env = NewBuilder(environment.FileSystem(fs))
	})

	AfterEach(func() {
		Expect(env.Cleanup()).To(Succeed())
	})

	Context("pruning resources", func() {
		var resourceObj *v1alpha1.Resource
		var namespace *corev1.Namespace
		var ctfName, componentName, resourceName, deployerObjName string
		var componentVersion1, componentVersion2 string

		BeforeEach(func(ctx SpecContext) {
			ctfName = "ctf-prune-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentName = "ocm.software/test-component-prune-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			resourceName = "test-yamlstream-prune-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			deployerObjName = "test-deployer-prune-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentVersion1 = "v1.0.0"
			componentVersion2 = "v2.0.0"

			namespaceName := test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		})

		AfterEach(func(ctx SpecContext) {
			if resourceObj != nil {
				_ = k8sClient.Delete(ctx, resourceObj)
			}
			_ = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm-1", Namespace: namespace.GetName()}})
			_ = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm-2", Namespace: namespace.GetName()}})
		})

		It("prunes resources that are removed from the manifest", func(ctx SpecContext) {
			resourceType := artifacttypes.PLAIN_TEXT

			// v1: Contains cm-1 and cm-2
			yamlStream1 := []byte(fmt.Sprintf(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-1
  namespace: %[1]s
data:
  ver: v1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-2
  namespace: %[1]s
data:
  ver: v1
`, namespace.GetName()))

			// v2: Contains only cm-1
			yamlStream2 := []byte(fmt.Sprintf(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-1
  namespace: %[1]s
data:
  ver: v2
`, namespace.GetName()))

			env.OCMCommonTransport(ctfName, accessio.FormatDirectory, func() {
				env.Component(componentName, func() {
					env.Version(componentVersion1, func() {
						env.Resource(resourceName, componentVersion1, resourceType, ocmmetav1.LocalRelation, func() {
							env.BlobData(mime.MIME_YAML, yamlStream1)
						})
					})
					env.Version(componentVersion2, func() {
						env.Resource(resourceName, componentVersion2, resourceType, ocmmetav1.LocalRelation, func() {
							env.BlobData(mime.MIME_YAML, yamlStream2)
						})
					})
				})
			})

			spec, err := ctf.NewRepositorySpec(ctf.ACC_READONLY, filepath.Join(tempDir, ctfName))
			Expect(err).NotTo(HaveOccurred())
			specData, err := spec.MarshalJSON()
			Expect(err).NotTo(HaveOccurred())

			// 1. Deploy v1
			resourceObj = test.MockResource(
				ctx,
				resourceName,
				namespace.GetName(),
				&test.MockResourceOptions{
					ComponentRef: corev1.LocalObjectReference{Name: componentName},
					Clnt:         k8sClient,
					Recorder:     recorder,
					ComponentInfo: &v1alpha1.ComponentInfo{
						Component:      componentName,
						Version:        componentVersion1,
						RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
					},
					ResourceInfo: &v1alpha1.ResourceInfo{
						Name:    resourceName,
						Type:    resourceType,
						Version: componentVersion1,
						Access:  apiextensionsv1.JSON{Raw: []byte("{}")},
					},
				},
			)
			// Helper to update resource object
			updateResource := func(ver string, data []byte) {
				hashSum := sha256.Sum256(data)
				hash := fmt.Sprintf("SHA-256:%s[%s]", hex.EncodeToString(hashSum[:]), "genericBlobDigest/v1")

				test.MockResource(
					ctx,
					resourceName,
					namespace.GetName(),
					&test.MockResourceOptions{
						ComponentRef: corev1.LocalObjectReference{Name: componentName},
						Clnt:         k8sClient,
						Recorder:     recorder,
						ComponentInfo: &v1alpha1.ComponentInfo{
							Component:      componentName,
							Version:        ver,
							RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
						},
						ResourceInfo: &v1alpha1.ResourceInfo{
							Name:    resourceName,
							Type:    resourceType,
							Version: ver,
							Access:  apiextensionsv1.JSON{Raw: []byte("{}")},
							Digest:  hash,
						},
					},
				)
			}

			updateResource(componentVersion1, yamlStream1)

			deployerObj := &v1alpha1.Deployer{
				ObjectMeta: metav1.ObjectMeta{
					Name: deployerObjName,
				},
				Spec: v1alpha1.DeployerSpec{
					ResourceRef: v1alpha1.ObjectKey{
						Name:      resourceName,
						Namespace: namespace.GetName(),
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployerObj)).To(Succeed())

			By("waiting for v1 to be deployed")
			test.WaitForReadyObject(ctx, k8sClient, deployerObj, map[string]any{})

			By("checking cm-1 and cm-2 exist")
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: "cm-1", Namespace: namespace.GetName()}, &corev1.ConfigMap{})
			}).Should(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: "cm-2", Namespace: namespace.GetName()}, &corev1.ConfigMap{})
			}).Should(Succeed())

			By("updating to v2 (removing cm-2)")
			updateResource(componentVersion2, yamlStream2)

			By("waiting for deployer to be ready with v2")
			// We can check if status.Deployed has changed or if ObservedGeneration matches.
			// Or just check the result.

			Eventually(func() error {
				cm := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: "cm-1", Namespace: namespace.GetName()}, cm); err != nil {
					return err
				}
				if cm.Data["ver"] != "v2" {
					return fmt.Errorf("cm-1 version mismatch: got %s", cm.Data["ver"])
				}
				return nil
			}).Should(Succeed())

			By("checking cm-2 is pruned")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "cm-2", Namespace: namespace.GetName()}, &corev1.ConfigMap{})
				return errors.IsNotFound(err)
			}).Should(BeTrue(), "cm-2 should be deleted")
		})
	})
})
