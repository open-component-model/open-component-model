package deployer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "ocm.software/ocm/api/helper/builder"

	"github.com/mandelsoft/vfs/pkg/osfs"
	"github.com/mandelsoft/vfs/pkg/projectionfs"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	environment "ocm.software/ocm/api/helper/env"
	ocmmetav1 "ocm.software/ocm/api/ocm/compdesc/meta/v1"
	"ocm.software/ocm/api/ocm/extensions/artifacttypes"
	"ocm.software/ocm/api/ocm/extensions/repositories/ctf"
	"ocm.software/ocm/api/utils/accessio"
	"ocm.software/ocm/api/utils/mime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/test"
)

var _ = Describe("Deployer Controller with YAML stream (ConfigMap + Secret)", func() {
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

	Context("deployer controller (yaml stream)", func() {
		var resourceObj *v1alpha1.Resource
		var namespace *corev1.Namespace
		var ctfName, componentName, resourceName, deployerObjName string
		var componentVersion string

		BeforeEach(func(ctx SpecContext) {
			ctfName = "ctf-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentName = "ocm.software/test-component-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			resourceName = "test-yamlstream-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			deployerObjName = "test-deployer-yaml-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentVersion = "v1.0.0"

			namespaceName := test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		})

		AfterEach(func(ctx SpecContext) {
			By("deleting the deployer resource object")
			if resourceObj != nil {
				Expect(k8sClient.Delete(ctx, resourceObj)).To(Succeed())
				Eventually(func(ctx context.Context) error {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObj)
					if errors.IsNotFound(err) {
						return nil
					}
					return fmt.Errorf("resource %s still exists", resourceObj.GetName())
				}).WithContext(ctx).Should(Succeed())
			}

			// Best-effort cleanup of applied workload objects (in case GC/owner-refs aren't set)
			_ = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
				Name:      "sample-cm",
				Namespace: namespace.GetName(),
			}})
			_ = k8sClient.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name:      "sample-secret",
				Namespace: namespace.GetName(),
			}})
		})

		It("reconciles a deployer that applies a YAML stream", func(ctx SpecContext) {
			By("creating a CTF with the YAML stream blob")
			resourceType := artifacttypes.PLAIN_TEXT
			resourceVersion := "1.0.0"

			// Multi-doc YAML stream: ConfigMap + Secret
			yamlStream := []byte(fmt.Sprintf(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: sample-cm
  namespace: %[1]s
data:
  hello: world
---
apiVersion: v1
kind: Secret
metadata:
  name: sample-secret
  namespace: %[1]s
type: Opaque
stringData:
  password: s3cr3t
`, namespace.GetName()))

			env.OCMCommonTransport(ctfName, accessio.FormatDirectory, func() {
				env.Component(componentName, func() {
					env.Version(componentVersion, func() {
						env.Resource(resourceName, resourceVersion, resourceType, ocmmetav1.LocalRelation, func() {
							// Store as text; controller treats it as YAML stream
							env.BlobData(mime.MIME_YAML, yamlStream)
						})
					})
				})
			})

			spec, err := ctf.NewRepositorySpec(ctf.ACC_READONLY, filepath.Join(tempDir, ctfName))
			Expect(err).NotTo(HaveOccurred())
			specData, err := spec.MarshalJSON()
			Expect(err).NotTo(HaveOccurred())

			By("mocking a Resource that references the YAML stream")
			hash := sha256.Sum256(yamlStream)
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
						Version:        componentVersion,
						RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
					},
					ResourceInfo: &v1alpha1.ResourceInfo{
						Name:    resourceName,
						Type:    resourceType,
						Version: resourceVersion,
						Access:  apiextensionsv1.JSON{Raw: []byte("{}")},
						Digest: &v2.Digest{
							HashAlgorithm:          "SHA-256",
							NormalisationAlgorithm: "genericBlobDigest/v1",
							Value:                  hex.EncodeToString(hash[:]),
						},
					},
				},
			)

			By("creating a Deployer that references the Resource")
			deployerObj := &v1alpha1.Deployer{
				ObjectMeta: metav1.ObjectMeta{
					Name: deployerObjName,
				},
				Spec: v1alpha1.DeployerSpec{
					ResourceRef: v1alpha1.ObjectKey{
						Name:      resourceObj.GetName(),
						Namespace: namespace.GetName(),
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployerObj)).To(Succeed())

			By("waiting until the Deployer is Ready")
			test.WaitForReadyObject(ctx, k8sClient, deployerObj, map[string]any{})

			By("verifying the ConfigMap has been applied")
			gotCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: namespace.GetName(),
				Name:      "sample-cm",
			}, gotCM)).To(Succeed())
			Expect(gotCM.Data).To(HaveKeyWithValue("hello", "world"))

			By("verifying the Secret has been applied")
			gotSec := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: namespace.GetName(),
				Name:      "sample-secret",
			}, gotSec)).To(Succeed())
			// stringData is converted by API server into data (base64); compare the decoded value.
			Expect(string(gotSec.Data["password"])).To(Equal("s3cr3t"))

			By("deleting the Deployer")
			test.DeleteObject(ctx, k8sClient, deployerObj)
		})
	})
})
