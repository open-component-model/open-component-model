package e2e

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

const (
	ComponentConstructor = "component-constructor.yaml"
	Bootstrap            = "bootstrap.yaml"
	Rgd                  = "rgd.yaml"
	Instance             = "instance.yaml"
	K8sManifest          = "k8s-manifest.yaml"
	PublicKey            = "ocm.software.pub"
	PrivateKey           = "ocm.software"
)

var _ = Describe("controller", func() {
	Context("examples", func() {
		for _, example := range examples {
			if example.Name() == "applyset-pruning" {
				// skipping applyset-pruning example since we test this in e2e_applyset_test.go
				continue
			}
			fInfo, err := os.Stat(filepath.Join(examplesDir, example.Name()))
			Expect(err).NotTo(HaveOccurred())
			if !fInfo.IsDir() {
				continue
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
				Expect(utils.DeployResource(ctx, filepath.Join(examplesDir, example.Name(), Bootstrap))).To(Succeed())
				name := ""

				if slices.Contains(files, K8sManifest) {
					// Delete first to ensure idempotency across multiple test runs
					_ = utils.DeleteServiceAccountClusterAdmin(ctx, "ocm-k8s-toolkit-controller-manager")
					Expect(utils.MakeServiceAccountClusterAdmin(ctx, "ocm-k8s-toolkit-system", "ocm-k8s-toolkit-controller-manager")).To(Succeed())
				}

				if slices.Contains(files, Rgd) {
					name = "rgd/" + example.Name()
					Expect(utils.WaitForResource(ctx, "create", timeout, name)).To(Succeed())

					Expect(utils.WaitForResource(ctx, "condition=ResourceGraphAccepted=true", timeout, name)).To(
						Succeed(),
						"The resource graph definition %s was not accepted which means the RGD is invalid", name,
					)
					Expect(
						utils.WaitForResource(ctx, "condition=KindReady=true", timeout, name)).To(
						Succeed(),
						"The kind for the resource graph definition %s is not ready, which means KRO wasn't able to install the CRD in the Cluster", name,
					)
					Expect(
						utils.WaitForResource(ctx, "condition=ControllerReady=true", timeout, name)).To(
						Succeed(),
						"The controller for the resource graph definition %s is not ready, which means KRO wasn't able to reconcile the CRD", name,
					)
					Expect(
						utils.WaitForResource(ctx, "condition=Ready=true", timeout, name)).To(
						Succeed(),
						"The final readiness condition was not set, which means KRO believes the RGD %s was not reconciled correctly", name,
					)
				}

				if slices.Contains(files, Instance) {
					By("creating an instance of the example")
					Expect(utils.DeployAndWaitForResource(
						ctx, filepath.Join(examplesDir, example.Name(), Instance),
						"condition=Ready=true",
						timeout,
					)).To(Succeed())
				}

				By("validating the example")
				name = "deployment.apps/" + example.Name() + "-podinfo"
				Expect(utils.WaitForResource(ctx, "create", timeout, name)).To(Succeed())
				Expect(utils.WaitForResource(ctx, "condition=Available", timeout, name)).To(Succeed())
				Expect(utils.WaitForResource(
					ctx, "condition=Ready=true",
					timeout,
					"pod", "-l", "app.kubernetes.io/name="+example.Name()+"-podinfo",
				)).To(Succeed())

				// Check for configuration and localization
				if strings.HasSuffix(example.Name(), "-configuration-localization") {
					By("validating the localization")
					Expect(utils.CompareResourceField(ctx,
						"pod -l app.kubernetes.io/name="+example.Name()+"-podinfo",
						"'{.items[0].spec.containers[0].image}'",
						strings.TrimLeft(imageRegistry, "http://")+"/stefanprodan/podinfo:6.9.1",
					)).To(Succeed())
					By("validating the configuration")
					Expect(utils.CompareResourceField(ctx,
						"pod -l app.kubernetes.io/name="+example.Name()+"-podinfo",
						"'{.items[0].spec.containers[0].env[?(@.name==\"PODINFO_UI_MESSAGE\")].value}'",
						example.Name(),
					)).To(Succeed())
				}
				if slices.Contains(files, K8sManifest) {
					Expect(utils.DeleteServiceAccountClusterAdmin(ctx, "ocm-k8s-toolkit-controller-manager")).To(Succeed())
				}
			})
		}
	})
})
