package e2e

import (
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

var _ = Describe("Replication Controller", func() {
	Context("using credentials", func() {

		privateRegistry := os.Getenv("PROTECTED_REGISTRY_URL")
		Expect(privateRegistry).NotTo(Equal(""), "PROTECTED_REGISTRY_URL must be set for credentials tests")

		//internalPrivateRegistry := os.Getenv("INTERNAL_PROTECTED_REGISTRY_URL")
		//Expect(privateRegistry).NotTo(Equal(""), "INTERNAL_PROTECTED_REGISTRY_URL must be set for credentials tests")

		testdata := filepath.Join(os.Getenv("PROJECT_DIR"), "test/e2e/testdata")

		BeforeEach(func(ctx SpecContext) {
			tmpDir := GinkgoT().TempDir()
			By("Creating a component version for " + ctx.SpecReport().LeafNodeText)
			ctfDir := filepath.Join(tmpDir, "ctf-"+ctx.SpecReport().LeafNodeText)

			cmdArgs := []string{
				"add",
				"componentversions",
				"--create",
				"--file", ctfDir,
				filepath.Join(testdata, ctx.SpecReport().LeafNodeText, "component-constructor.yaml"),
			}

			cmd := exec.CommandContext(ctx, "ocm", cmdArgs...)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			cmdArgs = []string{
				"--config", filepath.Join(testdata, ctx.SpecReport().LeafNodeText, ".ocmconfig"),
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
		})

		It("basic-auth", func(ctx SpecContext) {
			testName := ctx.SpecReport().LeafNodeText
			By("Bootstrapping the example " + testName)
			Expect(utils.DeployResource(ctx, filepath.Join(testdata, testName, "secrets.yaml"))).To(Succeed())
			Expect(utils.DeployResource(ctx, filepath.Join(testdata, testName, Bootstrap))).To(Succeed())

			rgdName := "rgd/" + ctx.SpecReport().LeafNodeText
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
			By("Bootstrapping the example " + testName)
			Expect(utils.DeployResource(ctx, filepath.Join(testdata, testName, "secrets.yaml"))).To(Succeed())
			Expect(utils.DeployResource(ctx, filepath.Join(testdata, testName, Bootstrap))).To(Succeed())

			rgdName := "rgd/" + ctx.SpecReport().LeafNodeText
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
