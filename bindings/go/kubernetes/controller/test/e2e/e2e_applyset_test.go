package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/test/utils"
)

const (
	BootstrapDeployer = "bootstrap-deployer.yaml"
)

var _ = Describe("ApplySet Pruning Tests", func() {
	Context("when testing pruning with OCM deployer", func() {
		// applyset-pruning lives at the flat legacy path in examplesDir.
		const applysetName = "applyset-pruning"
		applysetDir := filepath.Join(examplesDir, applysetName)

		reqFiles := []string{ComponentConstructor, Bootstrap, BootstrapDeployer}

		It("should deploy the example "+applysetName, func(ctx SpecContext) {
			By("validating the example directory " + applysetName)
			var files []string
			Expect(filepath.WalkDir(
				applysetDir,
				func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if d.IsDir() {
						return nil
					}
					files = append(files, d.Name())
					return nil
				})).To(Succeed())

			Expect(files).To(ContainElements(reqFiles), "required files %s not found in example directory %q", reqFiles, applysetName)

			By("creating and transferring a component version for " + applysetName)
			// If directory contains a private key, the component version must signed.
			signingKey := ""
			if slices.Contains(files, PrivateKey) {
				signingKey = filepath.Join(applysetDir, PrivateKey)
			}
			Expect(utils.PrepareOCMComponent(
				ctx,
				applysetName,
				filepath.Join(applysetDir, ComponentConstructor),
				imageRegistry,
				signingKey,
			)).To(Succeed())

			By("bootstrapping the example")
			Expect(utils.DeployResource(ctx, filepath.Join(applysetDir, Bootstrap))).To(Succeed())
			Expect(utils.DeployResourceWithoutCleanup(ctx, filepath.Join(applysetDir, BootstrapDeployer))).To(Succeed())

			name := ""

			By("waiting for the first deployment to be ready")
			name = "deployment.apps/" + applysetName + "-podinfo"
			Expect(utils.WaitForResource(ctx, "create", timeout, name)).To(Succeed())
			Expect(utils.WaitForResource(ctx, "condition=Available", timeout, name)).To(Succeed())
			Expect(utils.WaitForResource(
				ctx, "condition=Ready=true",
				timeout,
				"pod", "-l", "app.kubernetes.io/name="+applysetName+"-podinfo",
			)).To(Succeed())

			name = "deployment.apps/" + applysetName + "-podinfo-2"
			By("waiting for the second deployment to be ready")
			Expect(utils.WaitForResource(ctx, "create", timeout, name)).To(Succeed())
			Expect(utils.WaitForResource(ctx, "condition=Available", timeout, name)).To(Succeed())
			Expect(utils.WaitForResource(
				ctx, "condition=Ready=true",
				timeout,
				"pod", "-l", "app.kubernetes.io/name="+applysetName+"-podinfo",
			)).To(Succeed())

			By("updating the component version to remove podinfo-2 (testing pruning)")

			// Create v2 component
			Expect(utils.PrepareOCMComponent(
				ctx,
				applysetName+"-2",
				filepath.Join(applysetDir, "component-constructor-2.yaml"),
				imageRegistry,
				"", // No signing
			)).To(Succeed())

			// inline update semver of
			// kubectl patch component applyset-pruning-component \
			//  --type merge \
			//  -p '{"spec":{"semver":"2.0.0"}}'
			execCmd := exec.CommandContext(ctx, "kubectl", "patch",
				"component.delivery.ocm.software/"+applysetName+"-component",
				"--type", "merge",
				"-p", `{"spec":{"semver":"2.0.0"}}`,
				"-n", "default",
			)
			_, err := utils.Run(execCmd)
			Expect(err).NotTo(HaveOccurred(), "Patching Component semver should succeed")

			By("waiting for the Component to update to v2.0.0")
			componentName := "component.delivery.ocm.software/" + applysetName + "-component"
			Eventually(func() string {
				cmd := exec.CommandContext(ctx, "kubectl", "get", componentName, "-n", "default", "-o", "jsonpath={.status.component.version}")
				output, err := utils.Run(cmd)
				if err != nil {
					return ""
				}
				return strings.TrimSpace(string(output))
			}, timeout).Should(Equal("2.0.0"), "Component should update to version 2.0.0")

			By("waiting for the Resource to update")
			resourceName := "resource.delivery.ocm.software/" + applysetName + "-resource"
			Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout, resourceName)).To(Succeed())

			By("verifying that podinfo-2 deployment has been pruned")
			// podinfo-2 should no longer exist - check using label selector
			Eventually(func() int {
				cmd := exec.CommandContext(ctx, "kubectl", "get", "deployments", "-n", "default", "-l", "app=podinfo-2", "-o", "json")
				output, err := utils.Run(cmd)
				if err != nil {
					return -1
				}
				var result map[string]interface{}
				if err := json.Unmarshal(output, &result); err != nil {
					return -1
				}
				items, ok := result["items"].([]interface{})
				if !ok {
					return -1
				}
				return len(items)
			}, "1m").Should(Equal(0), "podinfo-2 deployment should be pruned")

			By("verifying that podinfo deployment still exists")
			Expect(utils.WaitForResource(ctx, "condition=Available", timeout, "deployment.apps/"+applysetName+"-podinfo")).To(Succeed())

			// delete deployer
			By("cleaning up the deployer")
			deployerName := "deployer.delivery.ocm.software/" + applysetName + "-deployer"
			Expect(utils.DeleteResource(ctx, timeout, deployerName)).To(Succeed())

			// make sure that the deployer is deleted
			By("waiting for the deployer to be deleted")
			Eventually(func() error {
				cmd := exec.CommandContext(ctx, "kubectl", "get", deployerName, "-n", "default")
				_, err := utils.Run(cmd)
				return err
			}, timeout).Should(HaveOccurred(), "Deployer should be deleted")

			// check that deployed resources are also deleted
			By("verifying that deployed resources are deleted")
			res := "deployment.apps/" + applysetName + "-podinfo"
			Eventually(func() error {
				cmd := exec.CommandContext(ctx, "kubectl", "get", res, "-n", "default")
				_, err := utils.Run(cmd)
				return err
			}, timeout).Should(HaveOccurred(), "Deployed resource %s should be deleted", res)
		})
	})
})
