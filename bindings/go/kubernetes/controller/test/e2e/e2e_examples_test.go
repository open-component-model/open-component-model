package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/test/utils"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
)

const (
	ComponentConstructor     = "component-constructor.yaml"
	Bootstrap                = "bootstrap.yaml"
	Manifests                = "manifests.yaml"
	Rgd                      = "rgd.yaml"
	Instance                 = "instance.yaml"
	K8sManifest              = "k8s-manifest.yaml"
	PublicKey                = "ocm.software.pub"
	PrivateKey               = "ocm.software"
	CrossplaneComposition    = "crossplane-composition.yaml"
	CrossplaneInstance       = "crossplane-instance.yaml"
)

// ignoreExamples lists examples that are tested elsewhere or should be skipped.
var ignoreExamples = map[string]struct{}{
	"applyset-pruning":   {}, // tested in e2e_applyset_test.go
	"replication-simple": {}, // tested in e2e_replication_test.go
	"kustomize":          {}, // container dir for argocd/fluxcd sub-examples, not a standalone example
}

var _ = Describe("controller", func() {
	Context("examples", func() {
		AfterEach(func() {
			if !CurrentSpecReport().Failed() {
				return
			}

			utils.DumpLogs("kro", "rgd")
			utils.DumpLogs("argocd", "applications.argoproj.io")
		})

		for _, example := range examples {
			if _, ok := ignoreExamples[example.Name()]; ok {
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

				if slices.Contains(files, Rgd) {
					name = "rgd/" + example.Name()
					Expect(utils.WaitForResource(ctx, "create", timeout, name)).To(Succeed())
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

					By("checking for ArgoCD Applications on the cluster")
					name = "applications.argoproj.io/" + example.Name()
					Expect(utils.WaitForResource(ctx, "create", timeout, name, "-n", "argocd")).To(Succeed())

					By("validating the ArgoCD Application is Synced")
					Expect(utils.WaitForResource(ctx, "jsonpath={.status.sync.status}=Synced", timeout, name, "-n", "argocd")).To(Succeed())

					name = "deployment.apps/" + example.Name() + "-podinfo"

					By("validating the ArgoCD-managed deployment in default-argocd")

					Expect(utils.WaitForResource(ctx, "create", timeout, name, "-n", "default-argocd")).To(Succeed())
					Expect(utils.WaitForResource(ctx, "condition=Available", timeout, name, "-n", "default-argocd")).To(Succeed())
					Expect(utils.WaitForResource(
						ctx, "condition=Ready=true",
						timeout,
						"pod", "-l", "app.kubernetes.io/name="+example.Name()+"-podinfo", "-n", "default-argocd",
					)).To(Succeed())

				}

				// Crossplane flow: apply XRD+Composition directly, then XR instance
				// Crossplane flow: the bootstrap already deployed the OCM Resource and
				// Deployer for crossplane-xrd alongside the kro ones. Here we just wait
				// for the XRD to be Established, then create the XR instance.
				if slices.Contains(files, CrossplaneComposition) &&
					slices.Contains(files, CrossplaneInstance) {
					crossplaneName := example.Name() + "-crossplane"

					By("waiting for XRD to be Established")
					xrdName, xrdErr := xrdNameFromComposition(filepath.Join(examplesDir, example.Name(), CrossplaneComposition))
					Expect(xrdErr).NotTo(HaveOccurred())
					Expect(utils.WaitForResource(ctx, "condition=Established=true", timeout,
						"compositeresourcedefinitions.apiextensions.crossplane.io/"+xrdName)).To(Succeed())

					By("creating the Crossplane XR instance")
					Expect(utils.DeployAndWaitForResource(
						ctx, filepath.Join(examplesDir, example.Name(), CrossplaneInstance),
						"condition=Ready=true",
						timeout,
					)).To(Succeed())

					By("validating the Crossplane FluxCD-managed deployment")
					name = "deployment.apps/" + crossplaneName + "-podinfo"
					Expect(utils.WaitForResource(ctx, "create", timeout, name)).To(Succeed())
					Expect(utils.WaitForResource(ctx, "condition=Available", timeout, name)).To(Succeed())
					Expect(utils.WaitForResource(
						ctx, "condition=Ready=true",
						timeout,
						"pod", "-l", "app.kubernetes.io/name="+crossplaneName+"-podinfo",
					)).To(Succeed())

					By("checking for Crossplane ArgoCD Application")
					argoCDAppName := "applications.argoproj.io/" + crossplaneName
					Expect(utils.WaitForResource(ctx, "create", timeout, argoCDAppName, "-n", "argocd")).To(Succeed())
					Expect(utils.WaitForResource(ctx, "jsonpath={.status.sync.status}=Synced", timeout, argoCDAppName, "-n", "argocd")).To(Succeed())

					By("validating the Crossplane ArgoCD-managed deployment")
					name = "deployment.apps/" + crossplaneName + "-podinfo"
					Expect(utils.WaitForResource(ctx, "create", timeout, name, "-n", "default-argocd")).To(Succeed())
					Expect(utils.WaitForResource(ctx, "condition=Available", timeout, name, "-n", "default-argocd")).To(Succeed())
					Expect(utils.WaitForResource(
						ctx, "condition=Ready=true",
						timeout,
						"pod", "-l", "app.kubernetes.io/name="+crossplaneName+"-podinfo", "-n", "default-argocd",
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
					expectedRegistry, err := looseref.ParseReference(imageRegistry + "/")
					Expect(err).NotTo(HaveOccurred())

					assertLocalizedImage := func(resource string) {
						image, err := utils.GetResourceField(ctx, resource, "'{.items[0].spec.containers[0].image}'")
						Expect(err).NotTo(HaveOccurred())
						ref, err := looseref.ParseReference(image)
						Expect(err).NotTo(HaveOccurred(), "container image %q is not a valid OCI reference", image)
						Expect(ref.Registry).To(Equal(expectedRegistry.Registry))
					}

					By("validating the fluxcd localization")
					assertLocalizedImage("pod -l app.kubernetes.io/name=" + example.Name() + "-podinfo")

					By("validating the FluxCD configuration (ui.message)")
					Expect(utils.CompareResourceField(ctx,
						"pod -l app.kubernetes.io/name="+example.Name()+"-podinfo",
						"'{.items[0].spec.containers[0].env[?(@.name==\"PODINFO_UI_MESSAGE\")].value}'",
						example.Name(),
					)).To(Succeed())

					By("validating the ArgoCD localization")
					assertLocalizedImage("pod -l app.kubernetes.io/name=" + example.Name() + "-podinfo -n default-argocd")

					By("validating the ArgoCD configuration (ui.message)")
					Expect(utils.CompareResourceField(ctx,
						"pod -l app.kubernetes.io/name="+example.Name()+"-podinfo -n default-argocd",
						"'{.items[0].spec.containers[0].env[?(@.name==\"PODINFO_UI_MESSAGE\")].value}'",
						example.Name(),
					)).To(Succeed())
				}
			})
		}
	})
})

// xrdNameFromComposition reads the metadata.name of the first
// CompositeResourceDefinition in a crossplane-composition.yaml file.
func xrdNameFromComposition(compositionPath string) (string, error) {
	data, err := os.ReadFile(compositionPath)
	if err != nil {
		return "", err
	}
	// Simple line-by-line parse: find the first "kind: CompositeResourceDefinition"
	// and the "  name:" line that follows it.
	inXRD := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "kind: CompositeResourceDefinition" {
			inXRD = true
			continue
		}
		if inXRD && strings.HasPrefix(trimmed, "name:") {
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
			return name, nil
		}
		// Reset on next document separator
		if trimmed == "---" {
			inXRD = false
		}
	}
	return "", fmt.Errorf("no CompositeResourceDefinition name found in %s", compositionPath)
}
