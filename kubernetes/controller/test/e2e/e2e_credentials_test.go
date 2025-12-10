package e2e

import (
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

var _ = Describe("Credentials E2E Tests", func() {
	Context("simple use-cases", func() {

		testdata := filepath.Join(os.Getenv("PROJECT_DIR"), "test/e2e/testdata")

		It("basic-auth", func(ctx SpecContext) {
			testName := ctx.SpecReport().LeafNodeText

			privateRegistry := os.Getenv("PROTECTED_REGISTRY_URL")
			Expect(privateRegistry).NotTo(Equal(""), "PROTECTED_REGISTRY_URL must be set for credentials tests")

			By("Preparing OCM with credentials for " + testName)
			prepareOCMwithCredentials(ctx, testName, testdata, privateRegistry)

			By("Bootstrapping the example " + testName)
			Expect(utils.DeployResource(ctx, filepath.Join(testdata, testName, Bootstrap))).To(Succeed())

			rgdName := "rgd/" + testName
			Expect(utils.WaitForResource(ctx, "create", timeout, rgdName)).To(Succeed())

			Expect(utils.WaitForResource(ctx, "condition=ResourceGraphAccepted=true", timeout, rgdName)).To(
				Succeed(),
				"The resource graph definition %s was not accepted which means the RGD is invalid", rgdName,
			)
			Expect(
				utils.WaitForResource(ctx, "condition=KindReady=true", timeout, rgdName)).To(
				Succeed(),
				"The kind for the resource graph definition %s is not ready, which means KRO wasn't able to install the CRD in the Cluster", rgdName,
			)
			Expect(
				utils.WaitForResource(ctx, "condition=ControllerReady=true", timeout, rgdName)).To(
				Succeed(),
				"The controller for the resource graph definition %s is not ready, which means KRO wasn't able to reconcile the CRD", rgdName,
			)
			Expect(
				utils.WaitForResource(ctx, "condition=Ready=true", timeout, rgdName)).To(
				Succeed(),
				"The final readiness condition was not set, which means KRO believes the RGD %s was not reconciled correctly", rgdName,
			)

			By("creating an instance of the example")
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(testdata, ctx.SpecReport().LeafNodeText, Instance), "condition=Ready=true", timeout)).To(Succeed())

			By("validating the example")
			deploymentName := "deployment.apps/" + testName + "-podinfo"
			Expect(utils.WaitForResource(ctx, "create", timeout, deploymentName)).To(Succeed())
			Expect(utils.WaitForResource(ctx, "condition=Available", timeout, deploymentName)).To(Succeed())
			Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout, "pod", "-l", "app.kubernetes.io/name="+testName+"-podinfo")).To(Succeed())
		})

		It("docker-config-json", func(ctx SpecContext) {
			testName := ctx.SpecReport().LeafNodeText

			// Use a different registry to make sure we don't use cached credentials
			privateRegistry := os.Getenv("PROTECTED_REGISTRY_URL2")
			Expect(privateRegistry).NotTo(Equal(""), "PROTECTED_REGISTRY_URL2 must be set for credentials tests")

			By("Preparing OCM with credentials for " + testName)
			prepareOCMwithCredentials(ctx, testName, testdata, privateRegistry)

			By("Bootstrapping the example " + testName)
			Expect(utils.DeployResource(ctx, filepath.Join(testdata, testName, Bootstrap))).To(Succeed())

			rgdName := "rgd/" + testName
			Expect(utils.WaitForResource(ctx, "create", timeout, rgdName)).To(Succeed())

			Expect(utils.WaitForResource(ctx, "condition=ResourceGraphAccepted=true", timeout, rgdName)).To(
				Succeed(),
				"The resource graph definition %s was not accepted which means the RGD is invalid", rgdName,
			)
			Expect(
				utils.WaitForResource(ctx, "condition=KindReady=true", timeout, rgdName)).To(
				Succeed(),
				"The kind for the resource graph definition %s is not ready, which means KRO wasn't able to install the CRD in the Cluster", rgdName,
			)
			Expect(
				utils.WaitForResource(ctx, "condition=ControllerReady=true", timeout, rgdName)).To(
				Succeed(),
				"The controller for the resource graph definition %s is not ready, which means KRO wasn't able to reconcile the CRD", rgdName,
			)
			Expect(
				utils.WaitForResource(ctx, "condition=Ready=true", timeout, rgdName)).To(
				Succeed(),
				"The final readiness condition was not set, which means KRO believes the RGD %s was not reconciled correctly", rgdName,
			)

			By("creating an instance of the example")
			Expect(utils.DeployAndWaitForResource(ctx, filepath.Join(testdata, ctx.SpecReport().LeafNodeText, Instance), "condition=Ready=true", timeout)).To(Succeed())

			By("validating the example")
			deploymentName := "deployment.apps/" + testName + "-podinfo"
			Expect(utils.WaitForResource(ctx, "create", timeout, deploymentName)).To(Succeed())
			Expect(utils.WaitForResource(ctx, "condition=Available", timeout, deploymentName)).To(Succeed())
			Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout, "pod", "-l", "app.kubernetes.io/name="+testName+"-podinfo")).To(Succeed())
		})
	})
})

func prepareOCMwithCredentials(ctx SpecContext, testName string, testdata string, privateRegistry string) {
	tmpDir := GinkgoT().TempDir()
	By("Creating a component version for " + testName)
	ctfDir := filepath.Join(tmpDir, "ctf-"+testName)

	cmdArgs := []string{
		"add",
		"componentversions",
		"--create",
		"--file", ctfDir,
		filepath.Join(testdata, testName, "component-constructor.yaml"),
	}

	cmd := exec.CommandContext(ctx, "ocm", cmdArgs...)
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	cmdArgs = []string{
		"--config", filepath.Join(testdata, testName, ".ocmconfig"),
		"transfer",
		"ctf",
		"--overwrite",
		"--enforce",
		ctfDir,
		privateRegistry,
	}

	cmd = exec.CommandContext(ctx, "ocm", cmdArgs...)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())
}
