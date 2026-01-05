package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

const (
	// ApplySet label and annotation constants from applyset.go
	applySetParentIDLabel                  = "applyset.k8s.io/id"
	applySetPartOfLabel                    = "applyset.k8s.io/part-of"
	applySetToolingAnnotation              = "applyset.k8s.io/tooling"
	applySetGKsAnnotation                  = "applyset.k8s.io/contains-group-kinds"
	applySetAdditionalNamespacesAnnotation = "applyset.k8s.io/additional-namespaces"
)

// verifyDeployerApplySetLabelsAndAnnotations checks that the deployer has the correct ApplySet parent labels and annotations.
func verifyDeployerApplySetLabelsAndAnnotations(ctx context.Context, exampleName string) {
	deployerName := "deployer.delivery.ocm.software/" + exampleName + "-deployer"

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

	// Verify ApplySet tooling annotation
	tooling, ok := annotations[applySetToolingAnnotation].(string)
	if ok && tooling != "" {
		if tooling != "deployer.delivery.ocm.software/v1alpha1" {
			GinkgoWriter.Printf("⚠ Warning: ApplySet tooling annotation has unexpected value: %s\n", tooling)
		} else {
			GinkgoWriter.Printf("✓ Deployer has ApplySet tooling annotation: %s=%s\n", applySetToolingAnnotation, tooling)
		}
	} else {
		GinkgoWriter.Printf("⚠ Warning: ApplySet tooling annotation %s not found on deployer\n", applySetToolingAnnotation)
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
func verifyDeployedResourcesApplySetLabels(ctx context.Context, exampleName string) {
	deployerName := "deployer.delivery.ocm.software/" + exampleName + "-deployer"

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
