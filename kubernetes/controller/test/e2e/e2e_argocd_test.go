package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

var _ = Describe("ArgoCD Example Tests", func() {
	Context("when deploying a Helm chart through an ArgoCD Application", func() {
		const exampleName = "argocd-helm"

		var example os.DirEntry
		for _, e := range examples {
			if e.Name() != exampleName {
				continue
			}
			fInfo, err := os.Stat(filepath.Join(examplesDir, e.Name()))
			Expect(err).NotTo(HaveOccurred())
			if !fInfo.IsDir() {
				continue
			}
			example = e
		}

		AfterEach(func(ctx SpecContext) {
			if !CurrentSpecReport().Failed() {
				return
			}

			utils.DumpLogs("kro", "rgd")

			dump := func(label string, args ...string) {
				out, err := utils.Run(exec.CommandContext(ctx, "kubectl", args...))
				if err != nil {
					GinkgoLogr.Info(fmt.Sprintf("[DIAG] %s: error: %v", label, err))
					return
				}
				GinkgoLogr.Info(fmt.Sprintf("[DIAG] %s:\n%s", label, string(out)))
			}

			dump("argocd-application", "get", "application", "-n", "argocd",
				exampleName+"-application", "-o", "yaml")
			dump("podinfo-deployments", "get", "deploy", "-A", "-l", "app.kubernetes.io/instance="+exampleName)
			dump("all-podinfo", "get", "deploy,po", "-A", "--field-selector", "metadata.namespace!=kube-system")
		})

		reqFiles := []string{ComponentConstructor, Bootstrap, Rgd, Instance}

		It("should deploy the example "+exampleName, func(ctx SpecContext) {
			Expect(example).NotTo(BeNil(), "example directory %q not found", exampleName)

			By("validating the example directory " + exampleName)
			var files []string
			Expect(filepath.WalkDir(
				filepath.Join(examplesDir, exampleName),
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

			Expect(files).To(ContainElements(reqFiles), "required files %s not found in example directory %q", reqFiles, exampleName)

			By("creating and transferring a component version for " + exampleName)
			Expect(utils.PrepareOCMComponent(
				ctx,
				exampleName,
				filepath.Join(examplesDir, exampleName, ComponentConstructor),
				imageRegistry,
				"",
			)).To(Succeed())

			By("bootstrapping the example")
			Expect(utils.DeployResource(ctx, filepath.Join(examplesDir, exampleName, Bootstrap))).To(Succeed())

			By("waiting for the RGD to be ready")
			rgdName := "rgd/" + exampleName
			Expect(utils.WaitForResource(ctx, "create", timeout, rgdName)).To(Succeed())
			Expect(
				utils.WaitForResource(ctx, "condition=Ready=true", timeout, rgdName)).To(
				Succeed(),
				"The final readiness condition was not set, which means KRO believes the RGD %s was not reconciled correctly", rgdName,
			)

			By("creating an instance of the example")
			Expect(utils.DeployAndWaitForResource(
				ctx, filepath.Join(examplesDir, exampleName, Instance),
				"condition=Ready=true",
				timeout,
			)).To(Succeed())

			By("waiting for the ArgoCD Application to sync")
			appName := "application.argoproj.io/" + exampleName + "-application"
			Expect(utils.WaitForResource(ctx, "create", timeout, "-n", "argocd", appName)).To(Succeed())
			Expect(utils.WaitForResource(
				ctx, "jsonpath={.status.sync.status}=Synced", timeout, "-n", "argocd", appName,
			)).To(Succeed())
			Expect(utils.WaitForResource(
				ctx, "jsonpath={.status.health.status}=Healthy", timeout, "-n", "argocd", appName,
			)).To(Succeed())

			By("validating the deployment")
			deployment := "deployment.apps/" + exampleName + "-podinfo"
			Expect(utils.WaitForResource(ctx, "create", timeout, deployment)).To(Succeed())
			Expect(utils.WaitForResource(ctx, "condition=Available", timeout, deployment)).To(Succeed())
			Expect(utils.WaitForResource(
				ctx, "condition=Ready=true",
				timeout,
				"pod", "-l", "app.kubernetes.io/name="+exampleName+"-podinfo",
			)).To(Succeed())
		})
	})
})
