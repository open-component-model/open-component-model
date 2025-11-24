package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
		// GitHub credentials are required to access ghcr.io/open-component-model/ocm.
		It("should be possible to transfer the test component from its external location to configured OCI registry", func(ctx SpecContext) {
			By("Setting up GitHub credentials for ghcr.io")
			user, password, err := getUserAndPasswordForGitHub()
			if err != nil {
				Skip(fmt.Sprintf("Skipping test: %v", err))
			}

			ocmConfig := createGitHubOCMConfig(user, password)
			tmpDir := GinkgoT().TempDir()
			ocmConfigFile := filepath.Join(tmpDir, "ghcr-ocmconfig.yaml")
			Expect(os.WriteFile(ocmConfigFile, []byte(ocmConfig), 0644)).To(Succeed())
			indentedConfig := "    " + strings.ReplaceAll(strings.TrimSpace(ocmConfig), "\n", "\n    ")
			configMapYAML := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: ghcr-ocm-config
  namespace: %s
data:
  .ocmconfig: |
%s
`, testNamespace, indentedConfig)

			configMapFile := filepath.Join(tmpDir, "github-creds-configmap.yaml")
			Expect(os.WriteFile(configMapFile, []byte(configMapYAML), 0644)).To(Succeed())
			Expect(utils.DeployResource(ctx, configMapFile)).To(Succeed())
			sourceRepoYAML := fmt.Sprintf(`apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: source-repository1
  namespace: %s
spec:
  ocmConfig:
    - kind: ConfigMap
      name: ghcr-ocm-config
      policy: Propagate
  interval: 2m0s
  repositorySpec:
    baseUrl: ghcr.io/open-component-model/ocm
    type: OCIRegistry
`, testNamespace)

			sourceRepoFile := filepath.Join(tmpDir, "Repository-source.yaml")
			Expect(os.WriteFile(sourceRepoFile, []byte(sourceRepoYAML), 0644)).To(Succeed())

			componentYAML := fmt.Sprintf(`apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: podinfo1
  namespace: %s
spec:
  component: ocm.software/podinfo
  interval: 2m0s
  repositoryRef:
    name: source-repository1
  semver: 6.6.2
  ocmConfig:
    - apiVersion: delivery.ocm.software/v1alpha1
      kind: Repository
      name: source-repository1
      policy: Propagate
`, testNamespace)

			componentFile := filepath.Join(tmpDir, "Component.yaml")
			Expect(os.WriteFile(componentFile, []byte(componentYAML), 0644)).To(Succeed())

			By("Apply manifests to the cluster")
			manifestDir := filepath.Join(os.Getenv("PROJECT_DIR"), "test/e2e/testdata/replication-no-config")
			Expect(utils.DeployAndWaitForResource(ctx, sourceRepoFile, "condition=Ready", timeout)).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, componentFile, "condition=Ready", timeout)).To(Succeed())
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

			By("Setting up GitHub credentials for ghcr.io source")
			user, password, err := getUserAndPasswordForGitHub()
			if err != nil {
				Skip(fmt.Sprintf("Skipping test: %v", err))
			}

			ocmConfig := createGitHubOCMConfig(user, password)
			tmpDir := GinkgoT().TempDir()

			indentedConfig := "    " + strings.ReplaceAll(strings.TrimSpace(ocmConfig), "\n", "\n    ")
			configMapYAML := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: ghcr-ocm-config
  namespace: %s
data:
  .ocmconfig: |
%s
`, testNamespace, indentedConfig)

			configMapFile := filepath.Join(tmpDir, "github-creds-configmap.yaml")
			Expect(os.WriteFile(configMapFile, []byte(configMapYAML), 0644)).To(Succeed())
			Expect(utils.DeployResource(ctx, configMapFile)).To(Succeed())

			sourceRepoYAML := fmt.Sprintf(`apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: origin-repository
  namespace: %s
spec:
  ocmConfig:
    - kind: ConfigMap
      name: ghcr-ocm-config
      policy: Propagate
  interval: 2m0s
  repositorySpec:
    baseUrl: ghcr.io/open-component-model/ocm
    type: OCIRegistry
`, testNamespace)

			sourceRepoFile := filepath.Join(tmpDir, "Repository-source.yaml")
			Expect(os.WriteFile(sourceRepoFile, []byte(sourceRepoYAML), 0644)).To(Succeed())

			By("Apply manifests to the cluster, required for the first transfer operation")
			manifestDir := filepath.Join(os.Getenv("PROJECT_DIR"), "test/e2e/testdata/replication-with-config")
			Expect(utils.DeployResource(ctx, filepath.Join(manifestDir, "ConfigMap-transfer-opt.yaml"))).To(Succeed())
			Expect(utils.DeployResource(ctx, filepath.Join(manifestDir, "ConfigMap-creds1.yaml"))).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, sourceRepoFile, "condition=Ready", timeout)).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(manifestDir, "Component-origin.yaml"), "condition=Ready", timeout)).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(manifestDir, "Repository-intermediate.yaml"), "condition=Ready", timeout)).To(Succeed())
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(manifestDir, "Replication-to-intermediate.yaml"), "condition=Ready", timeout)).To(Succeed())

			By("Double-check that copied component version is present in the intermediate registry")
			ocmconfigFile := filepath.Join(manifestDir, "creds1.ocmconfig")
			componentReference := protectedRegistry + "//" + ocmCompName + ":" + ocmCompVersion
			Expect(utils.CheckOCMComponent(ctx, componentReference, ocmconfigFile, ocmCheckOptFailOnError)).To(Succeed())

			By("Apply manifests to the cluster, required for the second transfer operation")
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

// getUserAndPasswordForGitHub safely gets GitHub credentials for testing
func getUserAndPasswordForGitHub() (string, string, error) {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return "", "", fmt.Errorf("gh CLI not found: %w", err)
	}

	user, err := getGitHubUsername(gh)
	if err != nil {
		return "", "", fmt.Errorf("failed to get GitHub username: %w", err)
	}

	pw := exec.Command("sh", "-c", fmt.Sprintf("%s auth token", gh))
	out, err := pw.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("gh auth token failed: %w (output: %s)", err, out)
	}
	password := strings.TrimSpace(string(out))

	return user, password, nil
}

func getGitHubUsername(gh string) (string, error) {
	if githubUser := os.Getenv("GITHUB_USER"); githubUser != "" {
		return githubUser, nil
	}

	out, err := exec.Command("sh", "-c", fmt.Sprintf("%s api user", gh)).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh api user failed: %w (output: %s)", err, out)
	}
	structured := map[string]interface{}{}
	if err := json.Unmarshal(out, &structured); err != nil {
		return "", fmt.Errorf("failed to parse gh output: %w (output: %s)", err, out)
	}

	return structured["login"].(string), nil
}

// createGitHubOCMConfig creates an OCM config file with GitHub credentials
// using the consumer identity format (matching existing test configs)
func createGitHubOCMConfig(user, password string) string {
	return fmt.Sprintf(`type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: OCIRepository
          hostname: ghcr.io
          port: "443"
        credentials:
          - type: Credentials
            properties:
              username: %q
              password: %q
`, user, password)
}
