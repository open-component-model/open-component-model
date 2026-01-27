package utils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
)

// Run executes the provided command within this context.
func Run(cmd *exec.Cmd) ([]byte, error) {
	cmd.Dir = os.Getenv("PROJECT_DIR")

	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.Env = append(cmd.Env, "GO110MODULE=on")

	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(GinkgoWriter, "running: %s\n", command)
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

type DeployResourceOpts struct {
	IgnoreCleanupErr bool
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
			_, _ = fmt.Fprintf(GinkgoWriter, "warning: could not delete resource from manifest %s: %v\n", manifestFilePath, err)
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

func MakeServiceAccountClusterAdmin(ctx context.Context, serviceAccountNamespace string, serviceAccountName string) error {
	cmdArgs := []string{"create", "clusterrolebinding", fmt.Sprintf("%s-admin", serviceAccountName), "--clusterrole=cluster-admin", "--serviceaccount=" + serviceAccountNamespace + ":" + serviceAccountName}
	cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
	_, err := Run(cmd)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			// Service account already has cluster-admin role
			return nil
		}
		return err
	}
	DeferCleanup(func(ctx SpecContext) error {
		return deleteServiceAccountClusterAdmin(ctx, serviceAccountName)
	})
	return err
}

func deleteServiceAccountClusterAdmin(ctx context.Context, serviceAccountName string) error {
	cmdArgs := []string{"delete", "clusterrolebinding", fmt.Sprintf("%s-admin", serviceAccountName), "--ignore-not-found=true"}
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

// CheckOCMComponent executes the OCM CLI command 'ocm check cv' with the passed component reference.
// If credentials are required, the path to the OCM configuration file can be supplied as the second parameter.
// Options are optional. For possible values see:
// https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_check_componentversions.md
func CheckOCMComponent(ctx context.Context, componentReference, ocmConfigPath string, options ...string) error {
	c := []string{"ocm"}
	if len(ocmConfigPath) > 0 {
		c = append(c, "--config", ocmConfigPath)
	}
	c = append(c, "check", "cv")
	if len(options) > 0 {
		c = append(c, options[0:]...)
	}
	c = append(c, componentReference)

	cmd := exec.CommandContext(ctx, c[0], c[1:]...) //nolint:gosec // The argument list is constructed right above.
	if _, err := Run(cmd); err != nil {
		return err
	}

	return nil
}

// GetOCMResourceImageRef returns the image reference of a specified resource of a component version.
// For the format of component reference see OCM CLI documentation.
func GetOCMResourceImageRef(ctx context.Context, componentReference, resourceName, ocmConfigPath string) (string, error) {
	// Construct the command 'ocm get resources', which is used here to get the image reference of a resource.
	// See also: https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_get_resources.md
	c := []string{"ocm", "--loglevel", "error"}
	if len(ocmConfigPath) > 0 {
		c = append(c, "--config", ocmConfigPath)
	}
	c = append(c, "get", "resources", componentReference, resourceName, "-oJSON") // -oJSON is used to get the output in JSON format.

	cmd := exec.CommandContext(ctx, c[0], c[1:]...) //nolint:gosec // The argument list is constructed right above.
	output, err := Run(cmd)
	if err != nil {
		return "", err
	}

	// This struct corresponds to the json format of the command output.
	// We are only interested in one specific field, the image reference. All other fields are omitted.
	type Result struct {
		Items []struct {
			Element struct {
				Access struct {
					ImageReference string `json:"imageReference"`
				} `json:"access"`
			} `json:"element"`
		} `json:"items"`
	}

	var r Result
	err = json.Unmarshal(output, &r)
	if err != nil {
		return "", errors.New("could not unmarshal command output: " + string(output))
	}
	if len(r.Items) != 1 {
		return "", errors.New("exactly one item is expected in command output: " + string(output))
	}

	return r.Items[0].Element.Access.ImageReference, nil
}

// CreateNamespace creates Kubernetes namespace.
func CreateNamespace(ctx context.Context, ns string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "create", "ns", ns)
	_, err := Run(cmd)

	return err
}

// DeleteNamespace deletes Kubernetes namespace.
func DeleteNamespace(ctx context.Context, ns string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "delete", "ns", ns, "--ignore-not-found=true", "--cascade=foreground")
	_, err := Run(cmd)

	return err
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
