package e2e

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

var _ = Describe("Replication Controller", func() {
	Context("when transferring component versions (OCI)", func() {
		// Using an existing component for the test, either podinfo or OCM CLI itself.
		// podinfo is preferred, because it has an image, which can either be copied or not,
		// depending on provided transfer options.
		const (
			ocmCompName            = "ocm.software/podinfo" // "ocm.software/ocmcli"
			ocmCompVersion         = "6.6.2"                // "0.17.0"
			podinfoImage           = "stefanprodan/podinfo:6.6.2"
			podinfoImgResourceName = "image"
			ocmCheckOptFailOnError = "--fail-on-error"
		)

		const testNamespace = "e2e-replication-controller-test"

		const (
			envProtectedRegistryURL          = "PROTECTED_REGISTRY_URL"
			envInternalProtectedRegistryURL  = "INTERNAL_PROTECTED_REGISTRY_URL"
			envProtectedRegistryURL2         = "PROTECTED_REGISTRY_URL2"
			envInternalProtectedRegistryURL2 = "INTERNAL_PROTECTED_REGISTRY_URL2"
		)

		BeforeEach(func(ctx SpecContext) {
			Expect(utils.CreateNamespace(ctx, testNamespace))
			DeferCleanup(func(ctx SpecContext) error {
				return utils.DeleteNamespace(ctx, testNamespace)
			})
		})

		// This test transfers the test component from a public registry to the one configured in the test environment.
		// The test uses neither explicit transfer options nor credentials.
		It("should be possible to transfer the test component from its external location to configured OCI registry", func(ctx SpecContext) {
			By("Apply manifests to the cluster")
			manifestDir := filepath.Join(os.Getenv("PROJECT_DIR"), "test/e2e/testdata/replication-no-config")
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(manifestDir, "Repository-source.yaml"), "condition=Ready", timeout)).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(manifestDir, "Component.yaml"), "condition=Ready", timeout)).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(manifestDir, "Repository-target.yaml"), "condition=Ready", timeout)).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(manifestDir, "Replication.yaml"), "condition=Ready", timeout)).To(Succeed())

			By("Double-check that copied component version is present in the target repository")
			// Use external registry URL, because the check connects from outside of the cluster.
			Expect(utils.CheckOCMComponent(ctx, imageRegistry+"//"+ocmCompName+":"+ocmCompVersion, "")).To(Succeed())
		})

		// This test does two transfer operations:
		//   1. From a public registry to a private (intermediate) one configured in the test environment.
		//   2. From intermediate registry above to a yet another protected registry.
		// The protected registries are password-protected, thus respective ocmconfig are required to access them.
		// Also transfer options are used in both transfer operations.
		It("should be possible to transfer CVs between private OCI registries with transfer options", func(ctx SpecContext) {
			var (
				protectedRegistry          string
				internalProtectedRegistry  string
				protectedRegistry2         string
				internalProtectedRegistry2 string
			)

			By("Checking for protected registry URLs", func() {
				protectedRegistry = os.Getenv(envProtectedRegistryURL)
				Expect(protectedRegistry).NotTo(BeEmpty())
				internalProtectedRegistry = os.Getenv(envInternalProtectedRegistryURL)
				Expect(internalProtectedRegistry).NotTo(BeEmpty())
				protectedRegistry2 = os.Getenv(envProtectedRegistryURL2)
				Expect(protectedRegistry2).NotTo(BeEmpty())
				internalProtectedRegistry2 = os.Getenv(envInternalProtectedRegistryURL2)
				Expect(internalProtectedRegistry2).NotTo(BeEmpty())
			})

			By("Apply manifests to the cluster, required for the first transfer operation")
			manifestDir := filepath.Join(os.Getenv("PROJECT_DIR"), "test/e2e/testdata/replication-with-config")
			Expect(utils.DeployResource(ctx, filepath.Join(manifestDir, "ConfigMap-transfer-opt.yaml"))).To(Succeed())
			Expect(utils.DeployResource(ctx, filepath.Join(manifestDir, "ConfigMap-creds1.yaml"))).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(manifestDir, "Repository-source.yaml"), "condition=Ready", timeout)).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(manifestDir, "Component-origin.yaml"), "condition=Ready", timeout)).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(manifestDir, "Repository-intermediate.yaml"), "condition=Ready", timeout)).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(manifestDir, "Replication-to-intermediate.yaml"), "condition=Ready", timeout)).To(Succeed())

			By("Double-check that copied component version is present in the intermediate registry")
			// Credentials are required for the 'ocm check' command to access the protected registry.
			ocmconfigFile := filepath.Join(manifestDir, "creds1.ocmconfig")
			// Use external registry URL, because the check connects from outside.
			componentReference := protectedRegistry + "//" + ocmCompName + ":" + ocmCompVersion
			Expect(utils.CheckOCMComponent(ctx, componentReference, ocmconfigFile, ocmCheckOptFailOnError)).To(Succeed())

			By("Apply manifests to the cluster, required for the second transfer operation")
			// The intermediate repo is now the new source. Btw., the resource already exists in the cluster.
			Expect(utils.DeployResource(ctx, filepath.Join(manifestDir, "ConfigMap-creds2.yaml"))).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(manifestDir, "Component-intermediate.yaml"), "condition=Ready", timeout)).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(manifestDir, "Repository-target.yaml"), "condition=Ready", timeout)).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(manifestDir, "Replication-to-target.yaml"), "condition=Ready", timeout)).To(Succeed())

			By("Double-check that copied component version is present in the target registry")
			// Credentials are required for the 'ocm check' command to access the protected registry.
			ocmconfigFile = filepath.Join(manifestDir, "creds2.ocmconfig")
			// Use external registry URL, because the check connects from outside.
			componentReference = protectedRegistry2 + "//" + ocmCompName + ":" + ocmCompVersion
			Expect(utils.CheckOCMComponent(ctx, componentReference, ocmconfigFile, ocmCheckOptFailOnError)).To(Succeed())

			By("Double-check that \"resourcesByValue\" transfer option has been applied")
			// I.e. that the resource's imageReference points to the correct (target) registry .
			// Example reference:
			// "http://protected-registry2-internal.default.svc.cluster.local:5002/stefanprodan/podinfo:6.6.2@sha256:4aa3b819f4cafc97d03d902ed17cbec076e2beee02d53b67ff88527124086fd9"
			imgRef, err := utils.GetOCMResourceImageRef(ctx, componentReference, podinfoImgResourceName, ocmconfigFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.HasPrefix(imgRef, internalProtectedRegistry2+"/"+podinfoImage)).Should(BeTrue())
		})
	})
})
