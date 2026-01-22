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

	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

var _ = Describe("ApplySet Pruning Tests", func() {
	Context("when testing pruning with OCM deployer", func() {
		var example os.DirEntry
		for _, e := range examples {
			// skip other examples
			if e.Name() != "applyset-pruning" {
				continue
			}
			fInfo, err := os.Stat(filepath.Join(examplesDir, e.Name()))
			Expect(err).NotTo(HaveOccurred())
			if !fInfo.IsDir() {
				continue
			}
			example = e
		}

		reqFiles := []string{ComponentConstructor, Bootstrap}

		It("should deploy the example "+example.Name(), func(ctx SpecContext) {
			By("validating the example directory " + example.Name())
			var files []string
			Expect(filepath.WalkDir(
				filepath.Join(examplesDir, example.Name()),
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

			Expect(files).To(ContainElements(reqFiles), "required files %s not found in example directory %q", reqFiles, example.Name())

			By("creating and transferring a component version for " + example.Name())
			// If directory contains a private key, the component version must signed.
			signingKey := ""
			if slices.Contains(files, PrivateKey) {
				signingKey = filepath.Join(examplesDir, example.Name(), PrivateKey)
			}
			Expect(utils.PrepareOCMComponent(
				ctx,
				example.Name(),
				filepath.Join(examplesDir, example.Name(), ComponentConstructor),
				imageRegistry,
				signingKey,
			)).To(Succeed())

			By("bootstrapping the example")
			Expect(utils.DeployResourceIgnoreErrors(ctx, filepath.Join(examplesDir, example.Name(), Bootstrap))).To(Succeed())

			// Delete first to ensure idempotency across multiple test runs
			_ = utils.DeleteServiceAccountClusterAdmin(ctx, "ocm-k8s-toolkit-controller-manager")
			Expect(utils.MakeServiceAccountClusterAdmin(ctx, "ocm-k8s-toolkit-system", "ocm-k8s-toolkit-controller-manager")).To(Succeed())

			name := ""

			By("waiting for the first deployment to be ready")
			name = "deployment.apps/" + example.Name() + "-podinfo"
			Expect(utils.WaitForResource(ctx, "create", timeout, name)).To(Succeed())
			Expect(utils.WaitForResource(ctx, "condition=Available", timeout, name)).To(Succeed())
			Expect(utils.WaitForResource(
				ctx, "condition=Ready=true",
				timeout,
				"pod", "-l", "app.kubernetes.io/name="+example.Name()+"-podinfo",
			)).To(Succeed())

			name = "deployment.apps/" + example.Name() + "-podinfo-2"
			By("waiting for the second deployment to be ready")
			Expect(utils.WaitForResource(ctx, "create", timeout, name)).To(Succeed())
			Expect(utils.WaitForResource(ctx, "condition=Available", timeout, name)).To(Succeed())
			Expect(utils.WaitForResource(
				ctx, "condition=Ready=true",
				timeout,
				"pod", "-l", "app.kubernetes.io/name="+example.Name()+"-podinfo",
			)).To(Succeed())

			By("updating the component version to remove podinfo-2 (testing pruning)")

			// Create v2 component
			Expect(utils.PrepareOCMComponent(
				ctx,
				example.Name()+"-2",
				filepath.Join(examplesDir, example.Name(), "component-constructor-2.yaml"),
				imageRegistry,
				"", // No signing
			)).To(Succeed())

			// inline update semver of
			// kubectl patch component applyset-pruning-component \
			//  --type merge \
			//  -p '{"spec":{"semver":"2.0.0"}}'
			execCmd := exec.CommandContext(ctx, "kubectl", "patch",
				"component.delivery.ocm.software/"+example.Name()+"-component",
				"--type", "merge",
				"-p", `{"spec":{"semver":"2.0.0"}}`,
				"-n", "default",
			)
			_, err := utils.Run(execCmd)
			Expect(err).NotTo(HaveOccurred(), "Patching Component semver should succeed")

			By("waiting for the Component to update to v2.0.0")
			componentName := "component.delivery.ocm.software/" + example.Name() + "-component"
			Eventually(func() string {
				cmd := exec.CommandContext(ctx, "kubectl", "get", componentName, "-n", "default", "-o", "jsonpath={.status.component.version}")
				output, err := utils.Run(cmd)
				if err != nil {
					return ""
				}
				return strings.TrimSpace(string(output))
			}, timeout).Should(Equal("2.0.0"), "Component should update to version 2.0.0")

			By("waiting for the Resource to update")
			resourceName := "resource.delivery.ocm.software/" + example.Name() + "-resource"
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
			Expect(utils.WaitForResource(ctx, "condition=Available", timeout, "deployment.apps/"+example.Name()+"-podinfo")).To(Succeed())

			// delete deployer
			By("cleaning up the deployer")
			deployerName := "deployer.delivery.ocm.software/" + example.Name() + "-deployer"
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
			res := "deployment.apps/" + example.Name() + "-podinfo"
			Eventually(func() error {
				cmd := exec.CommandContext(ctx, "kubectl", "get", res, "-n", "default")
				_, err := utils.Run(cmd)
				return err
			}, timeout).Should(HaveOccurred(), "Deployed resource %s should be deleted", res)
		})
	})
})
