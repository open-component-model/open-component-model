package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
)

// Run executes the provided command within this context.
func Run(cmd *exec.Cmd) ([]byte, error) {
	cmd.Dir = os.Getenv("PROJECT_DIR")

	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.Env = append(cmd.Env, "GO110MODULE=on")

	command := strings.Join(cmd.Args, " ")
	GinkgoLogr.Info(fmt.Sprintf("Running: %s", command))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s failed with error: (%w) %s", command, err, string(output))
	}

	return output, nil
}

// DeployAndWaitForResource takes a manifest file of a k8s resource and deploys it with "kubectl". Correspondingly,
// a DeferCleanup-handler is created that will delete the resource, when the test-suite ends.
// Additionally, "waitingFor" is a resource condition to check if the resource was deployed successfully.
// Example:
//
//	err := DeployAndWaitForResource("./pod.yaml", "condition=Ready")
func DeployAndWaitForResource(ctx context.Context, manifestFilePath, waitingFor, timeout string) error {
	err := DeployResource(ctx, manifestFilePath)
	if err != nil {
		return err
	}

	return WaitForResource(ctx, waitingFor, timeout, "-f", manifestFilePath)
}

// DeployResource takes a manifest file of a k8s resource and deploys it with "kubectl". Correspondingly,
// a DeferCleanup-handler is created that will delete the resource, when the test-suite ends.
// In contrast to "DeployAndWaitForResource", this function does not wait for a certain condition to be fulfilled.
func DeployResource(ctx context.Context, manifestFilePath string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", manifestFilePath)
	_, err := Run(cmd)
	if err != nil {
		return err
	}
	DeferCleanup(func(ctx SpecContext) error {
		cmd = exec.CommandContext(ctx, "kubectl", "delete", "-f", manifestFilePath)
		_, err := Run(cmd)
		if err != nil {
			GinkgoLogr.V(3).Info("WARNING: failed to delete resource", "manifest", manifestFilePath)
		}

		return err
	})

	return err
}

// DeployResourceWithoutCleanup takes a manifest file of a k8s resource and deploys it with "kubectl".
// In contrast to "DeployResource", no DeferCleanup-handler is created to delete the resource afterwards.
func DeployResourceWithoutCleanup(ctx context.Context, manifestFilePath string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", manifestFilePath)
	_, err := Run(cmd)
	if err != nil {
		return err
	}
	return nil
}

// DeleteResource deletes one or more k8s resources with "kubectl".
// The resources to delete are passed as arguments.
// Additionally, a timeout can be specified, which is passed to "kubectl" as well.
func DeleteResource(ctx context.Context, timeout string, resource ...string) error {
	cmdArgs := append([]string{"delete"}, resource...)
	cmdArgs = append(cmdArgs, "--timeout="+timeout)
	cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
	_, err := Run(cmd)

	return err
}

func WaitForResource(ctx context.Context, condition, timeout string, resource ...string) error {
	cmdArgs := append([]string{"wait", "--for=" + condition}, resource...)
	cmdArgs = append(cmdArgs, "--timeout="+timeout)
	cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
	_, err := Run(cmd)

	return err
}

// PrepareOCMComponent creates an OCM component from a component-constructor file.
// After creating the OCM component, the component is transferred to imageRegistry.
func PrepareOCMComponent(ctx context.Context, name, componentConstructorPath, imageRegistry, signingKey string) error {
	By("creating ocm component for " + name)
	tmpDir := GinkgoT().TempDir()

	ctfDir := filepath.Join(tmpDir, "ctf")
	cmdArgs := []string{
		"add",
		"componentversions",
		"--create",
		"--file", ctfDir,
		componentConstructorPath,
	}

	cmd := exec.CommandContext(ctx, "ocm", cmdArgs...)
	_, err := Run(cmd)
	if err != nil {
		return fmt.Errorf("could not create ocm component: %w", err)
	}

	if signingKey != "" {
		By("signing ocm component for " + name)
		cmd = exec.CommandContext(ctx,
			"ocm",
			"sign",
			"componentversions",
			"--signature",
			"ocm.software",
			"--private-key",
			signingKey,
			ctfDir,
		)
		_, err := Run(cmd)
		if err != nil {
			return fmt.Errorf("could not create ocm component: %w", err)
		}
	}

	By("transferring ocm component for " + name)
	// Note: The option '--overwrite' is necessary, when a digest of a resource is changed or unknown (which is the case
	// in our default test)
	cmdArgs = []string{
		"transfer",
		"ctf",
		"--overwrite",
		"--enforce",
		"--copy-resources",
		"--omit-access-types",
		"gitHub",
		ctfDir,
		imageRegistry,
	}

	cmd = exec.CommandContext(ctx, "ocm", cmdArgs...)
	_, err = Run(cmd)
	if err != nil {
		return fmt.Errorf("could not transfer ocm component: %w", err)
	}

	return nil
}

// DumpLogs dumps pod logs and resource status for the given namespace and resource type.
// Intended for use in AfterEach to capture state on test failure.
// Creates its own context with a 30s timeout to survive parent context cancellation.
func DumpLogs(namespace, resourceType string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logLine := func(msg string) {
		GinkgoLogr.Info(msg)
	}

	return nil
}

// DumpLogs dumps pod logs and resource status for the given namespace and resource type.
// Intended for use in AfterEach to capture state on test failure.
// Creates its own context with a 30s timeout to survive parent context cancellation.
func DumpLogs(namespace, resourceType string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	GinkgoLogr.Info(fmt.Sprintf("=== Diagnostic dump: %s pods in namespace %s ===", resourceType, namespace))

	cmd := exec.CommandContext(ctx, "kubectl", "get", "pods", "-n", namespace, "-o", "wide")
	output, err := Run(cmd)
	if err != nil {
		GinkgoLogr.Info(fmt.Sprintf("Failed to get pods in %s: %v", namespace, err))
	} else {
		GinkgoLogr.Info(string(output))
	}

	cmd = exec.CommandContext(ctx, "kubectl", "logs", "-n", namespace, "--all-containers", "--tail=200", "-l", "app.kubernetes.io/name=kro")
	output, err = Run(cmd)
	if err != nil {
		GinkgoLogr.Info(fmt.Sprintf("Failed to get kro logs in %s: %v", namespace, err))
	} else {
		GinkgoLogr.Info(fmt.Sprintf("=== KRO logs (last 200 lines) ===\n%s", string(output)))
	}

	cmd = exec.CommandContext(ctx, "kubectl", "get", resourceType, "-o", "yaml")
	output, err = Run(cmd)
	if err != nil {
		GinkgoLogr.Info(fmt.Sprintf("Failed to get %s resources: %v", resourceType, err))
	} else {
		GinkgoLogr.Info(fmt.Sprintf("=== %s resource status ===\n%s", resourceType, string(output)))
	}
}

// CompareResourceField compares the value of a specific field in a Kubernetes resource
// with an expected value.
//
// Parameters:
// - resource: The Kubernetes resource to query (e.g., "pod my-pod").
// - fieldSelector: A JSONPath expression to select the field to compare.
// - expected: The expected value of the field.
//
// Returns:
// - An error if the field value does not match the expected value or if the command fails.
func CompareResourceField(ctx context.Context, resource, fieldSelector, expected string) error {
	args := []string{"get"}
	args = append(args, strings.Split(resource, " ")...)
	args = append(args, "-o", "jsonpath="+fieldSelector)
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	output, err := Run(cmd)
	if err != nil {
		return err
	}

	// Sanitize output
	result := strings.TrimSpace(string(output))
	result = strings.ReplaceAll(result, "'", "")

	if strings.TrimSpace(result) != expected {
		return fmt.Errorf("expected %s, got %s", expected, string(output))
	}

	return nil
}
