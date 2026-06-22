package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // Ginkgo DSL requires dot imports
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

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		GinkgoLogr.Info(fmt.Sprintf("> %s", line))
	}

	return output, nil
}

// DeployResource applies a manifest with kubectl and registers a DeferCleanup
// handler that deletes it (foreground cascading) when the spec ends.
func DeployResource(ctx context.Context, manifestFilePath string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", manifestFilePath)
	_, err := Run(cmd)
	if err != nil {
		return err
	}

	DeferCleanup(func(ctx SpecContext) error {
		cmd = exec.CommandContext(ctx, "kubectl", "delete", "--ignore-not-found", "--wait=true", "--timeout=5m", "--cascade=foreground", "-f", manifestFilePath)
		_, err = Run(cmd)
		if err != nil {
			GinkgoLogr.V(3).Info("WARNING: failed to delete resource", "manifest", manifestFilePath)
			return err
		}

		cmd = exec.CommandContext(ctx, "kubectl", "wait", "--for=delete", "--timeout=5m", "-f", manifestFilePath)
		_, err = Run(cmd)
		if err != nil {
			GinkgoLogr.V(3).Info("WARNING: failed waiting for delete resource", "manifest", manifestFilePath)
			return err
		}
		return nil
	})

	return err
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

type PrepareOCMComponentOptions struct {
	Name                     string
	ComponentConstructorPath string
	ImageRegistry            string
	SigningKey               string
	OCMConfig                string
	CopyResources            bool
}

func PrepareOCMComponentWithOptions(ctx context.Context, opts PrepareOCMComponentOptions) error {
	By("creating ocm component for " + opts.Name)
	tmpDir := GinkgoT().TempDir()

	ctfDir := filepath.Join(tmpDir, "ctf")
	cmdArgs := []string{
		"add",
		"componentversions",
		"--create",
		"--file", ctfDir,
		opts.ComponentConstructorPath,
	}

	cmd := exec.CommandContext(ctx, "ocm", cmdArgs...)
	_, err := Run(cmd)
	if err != nil {
		return fmt.Errorf("could not create ocm component: %w", err)
	}

	if opts.SigningKey != "" {
		By("signing ocm component for " + opts.Name)
		cmd = exec.CommandContext(ctx, //nolint:gosec // args are hardcoded in test code
			"ocm",
			"sign",
			"componentversions",
			"--signature",
			"ocm.software",
			"--private-key",
			opts.SigningKey,
			ctfDir,
		)
		_, err := Run(cmd)
		if err != nil {
			return fmt.Errorf("could not create ocm component: %w", err)
		}
	}

	By("transferring ocm component for " + opts.Name)
	// Note: The option '--overwrite' is necessary, when a digest of a resource is changed or unknown (which is the case
	// in our default test)
	cmdArgs = nil
	if opts.OCMConfig != "" {
		cmdArgs = append(cmdArgs, "--config", opts.OCMConfig)
	}
	cmdArgs = append(cmdArgs,
		"transfer",
		"ctf",
		"--overwrite",
		"--enforce",
	)
	if opts.CopyResources {
		cmdArgs = append(cmdArgs, "--copy-resources")
	}
	cmdArgs = append(cmdArgs,
		"--omit-access-types",
		"gitHub",
		ctfDir,
		opts.ImageRegistry,
	)

	cmd = exec.CommandContext(ctx, "ocm", cmdArgs...)
	_, err = Run(cmd)
	if err != nil {
		return fmt.Errorf("could not transfer ocm component: %w", err)
	}

	return nil
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
	result := strings.TrimSpace(strings.ReplaceAll(string(output), "'", ""))

	if result != expected {
		return fmt.Errorf("expected %s, got %s", expected, string(output))
	}

	return nil
}
