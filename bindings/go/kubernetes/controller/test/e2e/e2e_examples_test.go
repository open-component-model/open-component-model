package e2e

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/test/utils"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
)

const (
	ComponentConstructor = "component-constructor.yaml"
	Bootstrap            = "bootstrap.yaml"
	Manifests            = "manifests.yaml"
	Rgd                  = "rgd.yaml"
	Instance             = "instance.yaml"
	K8sManifest          = "k8s-manifest.yaml"
	PublicKey            = "ocm.software.pub"
	PrivateKey           = "ocm.software"
	CrossplaneComposition = "crossplane-composition.yaml"
)

// ignoreExamples lists example relative paths (from examplesDir) that are
// tested elsewhere or should be skipped. Both flat legacy names and new nested
// paths are accepted.
var ignoreExamples = map[string]struct{}{
	"applyset-pruning":   {}, // tested in e2e_applyset_test.go
	"replication-simple": {}, // tested in e2e_replication_test.go

}

// exampleEntry carries the absolute dir path and its path relative to
// examplesDir (slash-separated, used as the test name and for deriving K8s
// resource slugs).
type exampleEntry struct {
	// relPath is the slash-separated path relative to examplesDir, e.g.
	// "helm-simple" (legacy) or "helm/fluxcd/kro/simple" (new).
	relPath string
	// absDir is the absolute path to the scenario directory.
	absDir string
}

// slug converts a relPath to a Kubernetes-safe name by replacing "/" with "-".
func (e exampleEntry) slug() string {
	return strings.ReplaceAll(e.relPath, "/", "-")
}

// resourceName returns the canonical Kubernetes resource name used by this
// scenario's manifests. It is read from instance.yaml metadata.name when
// present (the most reliable source), falling back to slug().
// kro scenarios omit the operator sub-directory ("kro") from their resource
// names, so the slug is NOT the right string for kubectl waits.
func (e exampleEntry) resourceName() string {
	instPath := filepath.Join(e.absDir, Instance)
	data, err := os.ReadFile(instPath)
	if err != nil {
		return e.slug()
	}
	// Extract metadata.name with a simple line scan — avoids a YAML dependency.
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		}
	}
	return e.slug()
}

// walkExamples discovers all leaf scenario directories under root. A directory
// is a scenario if it contains component-constructor.yaml. The walker does not
// descend past a scenario directory.
func walkExamples(root string) []exampleEntry {
	var found []exampleEntry
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		if _, statErr := os.Stat(filepath.Join(path, ComponentConstructor)); statErr == nil {
			rel, _ := filepath.Rel(root, path)
			found = append(found, exampleEntry{
				relPath: filepath.ToSlash(rel),
				absDir:  path,
			})
			return fs.SkipDir
		}
		return nil
	})
	return found
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

		for _, example := range walkExamples(examplesDir) {
			example := example // capture

			if _, ok := ignoreExamples[example.relPath]; ok {
				continue
			}

			reqFiles := []string{ComponentConstructor, Bootstrap}

			It("should deploy the example "+example.relPath, func(ctx SpecContext) {
				exDir := example.absDir
				name := example.resourceName()

				By("collecting files in " + example.relPath)
				var files []string
				Expect(filepath.WalkDir(exDir, func(path string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if d.IsDir() {
						return nil
					}
					files = append(files, d.Name())
					return nil
				})).To(Succeed())

				Expect(files).To(ContainElements(reqFiles),
					"required files %s not found in example directory %q", reqFiles, example.relPath)

				By("creating and transferring a component version for " + example.relPath)
				signingKey := ""
				if slices.Contains(files, PrivateKey) {
					signingKey = filepath.Join(exDir, PrivateKey)
				}
				Expect(utils.PrepareOCMComponent(
					ctx,
					name,
					filepath.Join(exDir, ComponentConstructor),
					imageRegistry,
					signingKey,
				)).To(Succeed())

				// ----------------------------------------------------------------
				// Crossplane flow: the OCM Deployer in bootstrap.yaml applies the
				// XRD+Composition via the crossplane-xrd blob in the OCM component.
				// This is the correct OCM Kubernetes Toolkit pattern: the composition
				// flows through OCM (component → Resource → Deployer → cluster),
				// not applied directly from disk. After bootstrap we just wait for
				// the XRD to be Established before creating the XR instance.
				// ----------------------------------------------------------------
				isCrossplane := slices.Contains(files, CrossplaneComposition) &&
					slices.Contains(files, Instance)

				if isCrossplane {
					By("bootstrapping the crossplane example (OCM stack + XRD Deployer)")
					Expect(utils.DeployResource(ctx, filepath.Join(exDir, Bootstrap))).To(Succeed())

					By("waiting for OCM Repository and Component to be ready")
					Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout,
						"component.delivery.ocm.software/"+name+"-component", "-n", "default")).To(Succeed())

					By("waiting for XRD to be Established via OCM Deployer")
					xrdName := xrdNameFromComposition(filepath.Join(exDir, CrossplaneComposition))
					if xrdName != "" {
						Expect(utils.WaitForResource(ctx, "condition=Established=true", timeout,
							"compositeresourcedefinitions.apiextensions.crossplane.io/"+xrdName)).To(Succeed())
					}

					if slices.Contains(files, Instance) {
						By("creating the Crossplane XR instance")
						Expect(utils.DeployAndWaitForResource(
							ctx, filepath.Join(exDir, Instance),
							"condition=Ready=true",
							timeout,
						)).To(Succeed())

						// Register a DeferCleanup to force-unblock the Crossplane-composed
						// OCM Resource before the bootstrap cleanup runs. The composed
						// Resource gets an owner reference to the XR and a finalizer from
						// the OCM controller. When the XRD is deleted the owner becomes
						// a ghost, causing a finalizer deadlock. We remove the ownerRefs
						// and finalizers so the bootstrap delete can complete.
						composedResourceName := name + "-resource-chart"
						DeferCleanup(func(ctx SpecContext) {
							forceDeleteOCMResource(ctx, composedResourceName)
						})
					}

					// Crossplane scenarios that also drive ArgoCD check for an Application.
					if strings.Contains(example.relPath, "argocd") {
						appName := "applications.argoproj.io/" + name
						By("waiting for ArgoCD Application " + appName)
						Expect(utils.WaitForResource(ctx, "create", timeout, appName, "-n", "argocd")).To(Succeed())
						Expect(utils.WaitForResource(ctx, "jsonpath={.status.sync.status}=Synced", timeout, appName, "-n", "argocd")).To(Succeed())

						deployment := "deployment.apps/" + name + "-podinfo"
						By("validating ArgoCD-managed deployment " + deployment)
						Expect(utils.WaitForResource(ctx, "create", timeout, deployment, "-n", "default-argocd")).To(Succeed())
						Expect(utils.WaitForResource(ctx, "condition=Available", timeout, deployment, "-n", "default-argocd")).To(Succeed())
						Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout,
							"pod", "-l", "app.kubernetes.io/name="+name+"-podinfo", "-n", "default-argocd")).To(Succeed())
					} else {
						// Flux-backed crossplane scenario: final deployment lands in default.
						deployment := "deployment.apps/" + name + "-podinfo"
						By("validating Flux-managed deployment " + deployment)
						Expect(utils.WaitForResource(ctx, "create", timeout, deployment)).To(Succeed())
						Expect(utils.WaitForResource(ctx, "condition=Available", timeout, deployment)).To(Succeed())
						Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout,
							"pod", "-l", "app.kubernetes.io/name="+name+"-podinfo")).To(Succeed())
					}

					if strings.HasSuffix(example.relPath, "configuration-localization") {
						runLocalizationAssertions(ctx, name, example.relPath)
					}
					return
				}

				// ----------------------------------------------------------------
				// Legacy / kro flow (RGD + Instance + ArgoCD Application).
				// ----------------------------------------------------------------
				By("bootstrapping the example")
				Expect(utils.DeployResource(ctx, filepath.Join(exDir, Bootstrap))).To(Succeed())

				rgdName := ""
				if slices.Contains(files, Rgd) {
					rgdName = "rgd/" + name
					Expect(utils.WaitForResource(ctx, "create", timeout, rgdName)).To(Succeed())
					Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout, rgdName)).To(
						Succeed(),
						"RGD %s was not reconciled correctly", rgdName,
					)
				}

				if slices.Contains(files, Instance) {
					By("creating an instance of the example")
					Expect(utils.DeployAndWaitForResource(
						ctx, filepath.Join(exDir, Instance),
						"condition=Ready=true",
						timeout,
					)).To(Succeed())

					// Register a DeferCleanup that runs AFTER the instance DeferCleanup
					// (LIFO) but BEFORE the bootstrap DeferCleanup. It force-removes
					// the OCM finalizer from any kro-composed Resources so the bootstrap
					// --wait=true delete doesn't deadlock waiting for a Component that
					// is already gone.
					scenarioName := name
					DeferCleanup(func(ctx SpecContext) {
						forceCleanupKroComposedResources(ctx, scenarioName)
					})

					if strings.Contains(example.relPath, "argocd") {
						appName := "applications.argoproj.io/" + name
						By("checking for ArgoCD Application " + appName)
						Expect(utils.WaitForResource(ctx, "create", timeout, appName, "-n", "argocd")).To(Succeed())
						Expect(utils.WaitForResource(ctx, "jsonpath={.status.sync.status}=Synced", timeout, appName, "-n", "argocd")).To(Succeed())

						deployment := "deployment.apps/" + name + "-podinfo"
						By("validating ArgoCD-managed deployment " + deployment)
						Expect(utils.WaitForResource(ctx, "create", timeout, deployment, "-n", "default-argocd")).To(Succeed())
						Expect(utils.WaitForResource(ctx, "condition=Available", timeout, deployment, "-n", "default-argocd")).To(Succeed())
						Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout,
							"pod", "-l", "app.kubernetes.io/name="+name+"-podinfo", "-n", "default-argocd")).To(Succeed())
					}
				}

				By("validating the example")
				deployment := "deployment.apps/" + name + "-podinfo"
				if !strings.Contains(example.relPath, "argocd") {
					Expect(utils.WaitForResource(ctx, "create", timeout, deployment)).To(Succeed())
					Expect(utils.WaitForResource(ctx, "condition=Available", timeout, deployment)).To(Succeed())
					Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout,
						"pod", "-l", "app.kubernetes.io/name="+name+"-podinfo")).To(Succeed())
				}

				if strings.HasSuffix(example.relPath, "configuration-localization") {
					runLocalizationAssertions(ctx, name, example.relPath)
				}
			})
		}
	})
})

// runLocalizationAssertions validates image registry localization and the
// PODINFO_UI_MESSAGE env var for configuration-localization examples.
func runLocalizationAssertions(ctx SpecContext, name, relPath string) {
	expectedRegistry, err := looseref.ParseReference(imageRegistry + "/")
	Expect(err).NotTo(HaveOccurred())

	assertLocalizedImage := func(resource string) {
		image, err := utils.GetResourceField(ctx, resource, "'{.items[0].spec.containers[0].image}'")
		Expect(err).NotTo(HaveOccurred())
		ref, err := looseref.ParseReference(image)
		Expect(err).NotTo(HaveOccurred(), "container image %q is not a valid OCI reference", image)
		Expect(ref.Registry).To(Equal(expectedRegistry.Registry))
	}

	By("validating the FluxCD localization")
	if !strings.Contains(relPath, "argocd") {
		assertLocalizedImage("pod -l app.kubernetes.io/name=" + name + "-podinfo")

		By("validating the FluxCD configuration (ui.message)")
		Expect(utils.CompareResourceField(ctx,
			"pod -l app.kubernetes.io/name="+name+"-podinfo",
			"'{.items[0].spec.containers[0].env[?(@.name==\"PODINFO_UI_MESSAGE\")].value}'",
			name,
		)).To(Succeed())
	}

	if strings.Contains(relPath, "argocd") {
		By("validating the ArgoCD localization")
		assertLocalizedImage("pod -l app.kubernetes.io/name=" + name + "-podinfo -n default-argocd")

		By("validating the ArgoCD configuration (ui.message)")
		Expect(utils.CompareResourceField(ctx,
			"pod -l app.kubernetes.io/name="+name+"-podinfo -n default-argocd",
			"'{.items[0].spec.containers[0].env[?(@.name==\"PODINFO_UI_MESSAGE\")].value}'",
			name,
		)).To(Succeed())
	}
}

// xrdNameFromComposition reads the metadata.name of the first
// CompositeResourceDefinition in a crossplane-composition.yaml file.
// Returns "" if the file cannot be read or no XRD is found.
func xrdNameFromComposition(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	inXRD := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "kind: CompositeResourceDefinition") {
			inXRD = true
			continue
		}
		if inXRD && strings.HasPrefix(trimmed, "name:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
		}
		// Reset on next top-level "---" separator
		if trimmed == "---" {
			inXRD = false
		}
	}
	return ""
}

// forceDeleteOCMResource breaks the OCM finalizer chain for a Crossplane-managed
// scenario. The composed OCM Resource gets an owner reference to the Crossplane XR
// and a finalizer from the OCM controller. When the XRD is deleted the XR owner
// becomes a ghost, causing a deadlock. This function:
//  1. Removes the ownerReference and finalizer from the OCM Resource (stops the deadlock).
//  2. Deletes the OCM Resource so the Component can proceed.
//  3. Removes the Component finalizer so the Repository cleanup can proceed.
//
// All errors are ignored — this is best-effort cleanup running inside DeferCleanup.
func forceDeleteOCMResource(ctx context.Context, resourceName string) {
	// Derive sibling OCM object names from the resource-chart suffix.
	// resource-chart → strip "-resource-chart" suffix to get the base name.
	baseName := strings.TrimSuffix(resourceName, "-resource-chart")
	componentName := baseName + "-component"

	patchRemoveOwnerAndFinalizer := `[{"op":"replace","path":"/metadata/ownerReferences","value":[]},{"op":"replace","path":"/metadata/finalizers","value":[]}]`

	// 1. Clear the composed OCM Resource so Crossplane/OCM can proceed.
	cmd := exec.CommandContext(ctx, "kubectl", "patch",
		"resource.delivery.ocm.software/"+resourceName,
		"-n", "default",
		"--type", "json",
		"-p", patchRemoveOwnerAndFinalizer,
	)
	_, _ = utils.Run(cmd)

	cmd = exec.CommandContext(ctx, "kubectl", "delete",
		"resource.delivery.ocm.software/"+resourceName,
		"-n", "default",
		"--ignore-not-found",
	)
	_, _ = utils.Run(cmd)

	// 2. Remove the Component finalizer so the Repository can be deleted.
	// The Component may recreate the Resource object; removing its finalizer
	// allows it to be garbage-collected without a blocking dependency.
	cmd = exec.CommandContext(ctx, "kubectl", "patch",
		"component.delivery.ocm.software/"+componentName,
		"-n", "default",
		"--type", "json",
		"-p", `[{"op":"replace","path":"/metadata/finalizers","value":[]}]`,
	)
	_, _ = utils.Run(cmd)
}

// forceCleanupKroComposedResources removes OCM Resource finalizers that kro
// creates as composed objects from the kro RGD template. Without this, deleting
// bootstrap.yaml with --wait=true deadlocks: kro's composed OCM Resource holds
// finalizers.ocm.software/resource, the OCM controller can't remove it because
// the Component (also in bootstrap) is already gone, so kubectl delete
// bootstrap --wait blocks forever.
//
// Pattern: the kro RGD creates an OCM Resource named "<prefix>-resource-chart-name"
// (and similar variants for image, etc.). We patch out the finalizer so the
// resource can be GC'd cleanly.
//
// All errors are ignored — best-effort; the real test already passed.
func forceCleanupKroComposedResources(ctx context.Context, namePrefix string) {
	suffixes := []string{
		"-resource-chart-name",
		"-resource-image-name",
		"-resource-rgd",
	}
	for _, suffix := range suffixes {
		rName := namePrefix + suffix
		cmd := exec.CommandContext(ctx, "kubectl", "patch",
			"resource.delivery.ocm.software/"+rName,
			"-n", "default",
			"--type", "json",
			"-p", `[{"op":"replace","path":"/metadata/finalizers","value":[]}]`,
		)
		_, _ = utils.Run(cmd)
	}
}
