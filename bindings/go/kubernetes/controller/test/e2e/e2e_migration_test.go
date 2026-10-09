package e2e

import (
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/test/utils"
)

const (
	MigrationDeployer           = "deployer.yaml"
	MigrationNamespacedDeployer = "namespaced-deployer.yaml"
)

// These specs back the documented migration paths from a Deployer to a NamespacedDeployer. Both must hand the
// deployed objects over without deleting or recreating them.
var _ = Describe("Deployer to NamespacedDeployer migration", func() {
	testdata := filepath.Join(os.Getenv("PROJECT_DIR"), "test/e2e/testdata")

	kubectl := func(ctx SpecContext, args ...string) {
		GinkgoHelper()
		_, err := utils.Run(exec.CommandContext(ctx, "kubectl", args...))
		Expect(err).NotTo(HaveOccurred())
	}

	field := func(ctx SpecContext, resource, jsonPath string) string {
		GinkgoHelper()
		value, err := utils.GetResourceField(ctx, resource, jsonPath)
		Expect(err).NotTo(HaveOccurred())
		return value
	}

	for _, mode := range []string{"unsuspend", "orphan"} {
		It("hands over deployed objects by "+mode, func(ctx SpecContext) {
			name := "deployer-migration-" + mode
			dir := filepath.Join(testdata, name)
			deployer := "deployer.delivery.ocm.software/" + name
			namespacedDeployer := "namespaceddeployer.delivery.ocm.software/" + name
			configMap := "configmap/" + name

			By("deploying with a Deployer")
			Expect(utils.PrepareOCMComponent(ctx, name, filepath.Join(dir, ComponentConstructor), imageRegistry, "")).To(Succeed())
			Expect(utils.DeployResource(ctx, filepath.Join(dir, Bootstrap))).To(Succeed())
			Expect(utils.DeployResourceWithoutCleanup(ctx, filepath.Join(dir, MigrationDeployer))).To(Succeed())
			// A failed spec leaves the Deployer behind, which blocks the Resource deletion in the bootstrap cleanup.
			DeferCleanup(func(ctx SpecContext) {
				_, _ = utils.Run(exec.CommandContext(ctx, "kubectl", "patch", deployer, "--type", "merge", "-p", `{"metadata":{"finalizers":null}}`))
				_, _ = utils.Run(exec.CommandContext(ctx, "kubectl", "delete", deployer, "--ignore-not-found", "--cascade=orphan"))
			})
			Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout, deployer)).To(Succeed())
			Expect(utils.WaitForResource(ctx, "create", timeout, configMap)).To(Succeed())
			uid := field(ctx, configMap, "{.metadata.uid}")
			oldApplySet := field(ctx, configMap, `{.metadata.labels.applyset\.kubernetes\.io/part-of}`)

			By("suspending the Deployer and releasing its objects from its ApplySet")
			kubectl(ctx, "patch", deployer, "--type", "merge", "-p", `{"spec":{"suspend":true}}`)
			kubectl(ctx, "label", "configmaps", "-l", "applyset.kubernetes.io/part-of="+oldApplySet, "applyset.kubernetes.io/part-of-")

			By("creating the NamespacedDeployer")
			Expect(utils.DeployResource(ctx, filepath.Join(dir, MigrationNamespacedDeployer))).To(Succeed())
			Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout, namespacedDeployer)).To(Succeed())

			By("verifying the NamespacedDeployer took over the ConfigMap")
			Eventually(func(ctx SpecContext) (string, error) {
				return utils.GetResourceField(ctx, configMap, "{.metadata.ownerReferences[*].kind}")
			}, timeout).WithContext(ctx).Should(Equal("NamespacedDeployer"))
			Expect(field(ctx, configMap, `{.metadata.labels.applyset\.kubernetes\.io/part-of}`)).NotTo(Equal(oldApplySet))

			By("removing the Deployer")
			switch mode {
			case "unsuspend":
				kubectl(ctx, "delete", deployer, "--wait=false")
				kubectl(ctx, "patch", deployer, "--type", "merge", "-p", `{"spec":{"suspend":false}}`)
			case "orphan":
				kubectl(ctx, "patch", deployer, "--type", "json", "-p", `[{"op":"remove","path":"/metadata/finalizers"}]`)
				kubectl(ctx, "delete", deployer, "--cascade=orphan", "--timeout="+timeout)
			}
			Eventually(func(ctx SpecContext) error {
				_, err := utils.Run(exec.CommandContext(ctx, "kubectl", "get", deployer))
				return err
			}, timeout).WithContext(ctx).Should(HaveOccurred(), "Deployer should be deleted")

			By("verifying the ConfigMap survived unchanged")
			Consistently(func(ctx SpecContext) (string, error) {
				return utils.GetResourceField(ctx, configMap, "{.metadata.uid}")
			}, "15s").WithContext(ctx).Should(Equal(uid))
			Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout, namespacedDeployer)).To(Succeed())
		})
	}
})
