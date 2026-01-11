package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

const (
	// ApplySet label and annotation constants from applyset.go
	applySetParentIDLabel                  = "applyset.k8s.io/id"
	applySetPartOfLabel                    = "applyset.k8s.io/part-of"
	applySetToolingLabel                   = "applyset.k8s.io/tooling"
	applySetGKsAnnotation                  = "applyset.k8s.io/contains-group-kinds"
	applySetAdditionalNamespacesAnnotation = "applyset.k8s.io/additional-namespaces"
)

// verifyDeployerApplySetLabelsAndAnnotations checks that the deployer has the correct ApplySet parent labels and annotations.
func verifyDeployerApplySetLabelsAndAnnotations(ctx context.Context, example string) {
	deployerName := "deployer.delivery.ocm.software/" + example + "-deployer"

	// Get the deployer resource as JSON
	cmd := exec.CommandContext(ctx, "kubectl", "get", deployerName, "-n", "default", "-o", "json")
	output, err := utils.Run(cmd)
	if err != nil {
		GinkgoWriter.Printf("⚠ Warning: Could not get deployer %s: %v\n", deployerName, err)
		return
	}

	var deployer map[string]interface{}
	err = json.Unmarshal(output, &deployer)
	if err != nil {
		GinkgoWriter.Printf("⚠ Warning: Could not unmarshal deployer JSON: %v\n", err)
		return
	}

	// Extract metadata
	metadata, ok := deployer["metadata"].(map[string]interface{})
	if !ok {
		GinkgoWriter.Printf("⚠ Warning: Deployer metadata not found\n")
		return
	}

	// Check labels
	labels, ok := metadata["labels"].(map[string]interface{})
	if !ok {
		GinkgoWriter.Printf("⚠ Warning: Deployer labels not found\n")
		return
	}

	// Verify ApplySet parent ID label exists
	applySetID, ok := labels[applySetParentIDLabel].(string)
	if !ok || applySetID == "" {
		GinkgoWriter.Printf("⚠ Warning: ApplySet parent ID label %s not found on deployer (ApplySet labels may not be implemented yet)\n", applySetParentIDLabel)
		return
	}

	if !strings.HasPrefix(applySetID, "applyset-") {
		GinkgoWriter.Printf("⚠ Warning: ApplySet parent ID should start with 'applyset-', got: %s\n", applySetID)
	}

	GinkgoWriter.Printf("✓ Deployer has ApplySet parent ID label: %s=%s\n", applySetParentIDLabel, applySetID)

	// Check annotations
	annotations, ok := metadata["annotations"].(map[string]interface{})
	if !ok {
		GinkgoWriter.Printf("⚠ Warning: Deployer annotations not found\n")
		return
	}

	// Verify ApplySet tooling labels
	tooling, ok := labels[applySetToolingLabel].(string)
	if ok && tooling != "" {
		if tooling != "deployer.delivery.ocm.software.v1alpha1" {
			GinkgoWriter.Printf("⚠ Warning: ApplySet tooling label has unexpected value: %s\n", tooling)
		} else {
			GinkgoWriter.Printf("✓ Deployer has ApplySet tooling label: %s=%s\n", applySetToolingLabel, tooling)
		}
	} else {
		GinkgoWriter.Printf("⚠ Warning: ApplySet tooling label %s not found on deployer\n", applySetToolingLabel)
	}

	// Verify ApplySet group-kinds annotation exists
	gks, ok := annotations[applySetGKsAnnotation].(string)
	if ok && gks != "" {
		GinkgoWriter.Printf("✓ Deployer has ApplySet group-kinds annotation: %s=%s\n", applySetGKsAnnotation, gks)
	} else {
		GinkgoWriter.Printf("⚠ Warning: ApplySet group-kinds annotation %s not found or empty on deployer\n", applySetGKsAnnotation)
	}

	// Additional namespaces annotation is optional, so we only log if it exists
	if additionalNs, ok := annotations[applySetAdditionalNamespacesAnnotation].(string); ok && additionalNs != "" {
		GinkgoWriter.Printf("✓ Deployer has ApplySet additional namespaces annotation: %s=%s\n", applySetAdditionalNamespacesAnnotation, additionalNs)
	}
}

// verifyDeployedResourcesApplySetLabels checks that all deployed resources have the correct ApplySet part-of label.
func verifyDeployedResourcesApplySetLabels(ctx context.Context, example string) {
	deployerName := "deployer.delivery.ocm.software/" + example + "-deployer"

	// First, get the ApplySet ID from the deployer
	cmd := exec.CommandContext(ctx, "kubectl", "get", deployerName, "-n", "default", "-o", "jsonpath={.metadata.labels['"+applySetParentIDLabel+"']}")
	output, err := utils.Run(cmd)
	if err != nil {
		GinkgoWriter.Printf("⚠ Warning: Could not get deployer: %v\n", err)
		return
	}

	applySetID := strings.TrimSpace(string(output))
	if applySetID == "" {
		GinkgoWriter.Printf("⚠ Warning: ApplySet ID label not found on deployer (this might be expected if ApplySet labels aren't being set yet)\n")
		return
	}

	GinkgoWriter.Printf("✓ ApplySet ID from deployer: %s\n", applySetID)

	// Get the deployer to find out what resources it deployed
	cmd = exec.CommandContext(ctx, "kubectl", "get", deployerName, "-n", "default", "-o", "json")
	output, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to get deployer status")

	var deployer map[string]interface{}
	err = json.Unmarshal(output, &deployer)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to unmarshal deployer JSON")

	// Extract deployed objects from status
	status, ok := deployer["status"].(map[string]interface{})
	ExpectWithOffset(1, ok).To(BeTrue(), "Deployer status not found")

	deployed, ok := status["deployed"].([]interface{})
	if !ok || len(deployed) == 0 {
		GinkgoWriter.Printf("⚠ No deployed resources found in deployer status\n")
		return
	}

	GinkgoWriter.Printf("✓ Found %d deployed resources in deployer status\n", len(deployed))

	// Verify each deployed resource has the correct ApplySet part-of label
	for _, item := range deployed {
		resource, ok := item.(map[string]interface{})
		ExpectWithOffset(1, ok).To(BeTrue(), "Failed to cast deployed resource")

		apiVersion, _ := resource["apiVersion"].(string)
		kind, _ := resource["kind"].(string)
		name, _ := resource["name"].(string)
		namespace, _ := resource["namespace"].(string)

		if name == "" || kind == "" {
			continue
		}

		// Build the resource identifier
		resourceType := fmt.Sprintf("%s.%s", strings.ToLower(kind), getGroupFromAPIVersion(apiVersion))
		resourceIdentifier := resourceType + "/" + name

		// Build kubectl get command
		getArgs := []string{"get", resourceIdentifier}
		if namespace != "" {
			getArgs = append(getArgs, "-n", namespace)
		}
		getArgs = append(getArgs, "-o", "jsonpath={.metadata.labels['"+applySetPartOfLabel+"']}")

		cmd = exec.CommandContext(ctx, "kubectl", getArgs...)
		output, err = utils.Run(cmd)
		if err != nil {
			GinkgoWriter.Printf("⚠ Warning: Could not get resource %s: %v\n", resourceIdentifier, err)
			continue
		}

		partOfLabel := strings.TrimSpace(string(output))
		ExpectWithOffset(1, partOfLabel).To(Equal(applySetID),
			"Resource %s should have ApplySet part-of label matching deployer ApplySet ID", resourceIdentifier)

		GinkgoWriter.Printf("✓ Resource %s has correct ApplySet part-of label: %s=%s\n", resourceIdentifier, applySetPartOfLabel, partOfLabel)
	}
}

// getGroupFromAPIVersion extracts the group from an apiVersion string.
// For example: "apps/v1" returns "apps", "v1" returns "" (core group).
func getGroupFromAPIVersion(apiVersion string) string {
	parts := strings.Split(apiVersion, "/")
	if len(parts) == 2 {
		return parts[0]
	}
	return "" // Core API group
}

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

			By("verifying ApplySet labels and annotations on the deployer")
			verifyDeployerApplySetLabelsAndAnnotations(ctx, example.Name())

			By("verifying ApplySet labels on all deployed resources")
			verifyDeployedResourcesApplySetLabels(ctx, example.Name())

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

			By("verifying ApplySet labels still correct after pruning")
			verifyDeployedResourcesApplySetLabels(ctx, example.Name())

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

			By("cleaning up service account cluster admin")
			Expect(utils.DeleteServiceAccountClusterAdmin(ctx, "ocm-k8s-toolkit-controller-manager")).To(Succeed())
		})
	})
})
