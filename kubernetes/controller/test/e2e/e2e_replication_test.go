package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

const replicationExample = "replication-simple"

var _ = Describe("Replication E2E Tests", func() {
	Context("when replicating a component into a target repository", func() {
		AfterEach(func() {
			if !CurrentSpecReport().Failed() {
				return
			}

			utils.DumpLogs("default", "replication")
		})

		It("transfers the source component version into the target repository", func(ctx SpecContext) {
			exampleDir := filepath.Join(examplesDir, replicationExample)
			component := "ocm.software/ocm-k8s-toolkit/examples/" + replicationExample

			By("creating and transferring the source component version")
			Expect(utils.PrepareOCMComponent(
				ctx,
				replicationExample,
				filepath.Join(exampleDir, ComponentConstructor),
				imageRegistry,
				"",
			)).To(Succeed())

			By("bootstrapping the replication example")
			Expect(utils.DeployResource(ctx, filepath.Join(exampleDir, Manifests))).To(Succeed())

			By("waiting for the source component to be ready")
			Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout,
				"component.delivery.ocm.software/"+replicationExample+"-component")).To(Succeed())

			By("waiting for the target repository to be ready")
			Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout,
				"repository.delivery.ocm.software/"+replicationExample+"-target")).To(Succeed())

			By("waiting for the replication to complete")
			replicationName := "replication.delivery.ocm.software/" + replicationExample
			Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout, replicationName)).To(Succeed())

			By("verifying the replication recorded the transferred version")
			Eventually(func() string {
				cmd := exec.CommandContext(ctx, "kubectl", "get", replicationName, "-n", "default",
					"-o", "jsonpath={.status.lastTransferredVersion}")
				out, err := utils.Run(cmd)
				if err != nil {
					return ""
				}

				return strings.TrimSpace(string(out))
			}, timeout).Should(Equal("1.0.0"))

			By("verifying the component version exists in the target repository")
			targetRegistry := os.Getenv("PROTECTED_REGISTRY_URL")
			Expect(targetRegistry).NotTo(BeEmpty(), "PROTECTED_REGISTRY_URL must be set for replication tests")

			cmd := exec.CommandContext(ctx, "ocm",
				"--config", filepath.Join(exampleDir, ".ocmconfig"),
				"get", "cv",
				targetRegistry+"//"+component+":1.0.0",
			)
			out, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "the replicated component version must be retrievable from the target repository")
			Expect(string(out)).To(ContainSubstring(component))
		})
	})
})
