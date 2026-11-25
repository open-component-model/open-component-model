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

	"sigs.k8s.io/yaml"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	signingspec "ocm.software/open-component-model/bindings/go/signing/v1alpha1/spec"
)

// OCMBinary returns the ocm CLI executable. Override via OCM_CLI when running
// against a non-standard binary path.
func OCMBinary() string {
	if v := os.Getenv("OCM_CLI"); v != "" {
		return v
	}
	return "ocm"
}

// PlainHTTPConfig writes an OCM config file into dir that lets the ocm CLI talk
// plain HTTP to every e2e registry. OCM rejects an http:// prefix unless the HTTP
// client configuration sets plainHTTP, so every CLI call against an e2e registry
// passes the returned path via --config. ghcr.io stays on HTTPS: the examples pull
// public images from it, and it refuses cleartext requests.
func PlainHTTPConfig(dir string) (string, error) {
	cfg := "type: generic.config.ocm.software/v1\nconfigurations:\n  - type: http.config.ocm.software/v1alpha1\n    plainHTTP: true\n    hosts:\n      ghcr.io:\n        plainHTTP: false\n"
	path := filepath.Join(dir, "plain-http.ocmconfig")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		return "", fmt.Errorf("could not write plain HTTP ocmconfig: %w", err)
	}
	return path, nil
}

// Run executes the provided command within this context.
func Run(cmd *exec.Cmd) ([]byte, error) {
	if cmd.Dir == "" {
		cmd.Dir = os.Getenv("PROJECT_DIR")
	}

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
		// No --timeout: the OCM finalizer teardown is a dependency cascade
		// (Deployer -> Resource -> Component -> Repository) whose controllers
		// return errors on the transient "still referenced" condition, so
		// controller-runtime's exponential backoff can stretch a brief ordering
		// conflict into several minutes. Let the Ginkgo spec deadline bound the
		// wait instead of a fixed timeout that flakes on slow teardowns (observed
		// exceeding 180s on the cross-namespace Repository finalizer).
		cmd = exec.CommandContext(ctx, "kubectl", "delete", "--ignore-not-found", "--wait=true", "-f", manifestFilePath)
		_, err := Run(cmd)
		if err != nil {
			GinkgoLogr.V(3).Info("WARNING: cleanup failed", "manifest", manifestFilePath)
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

// PrepareOCMComponent builds and transfers an OCM component for the named example
// by running the example's Taskfile: `task --taskfile <Taskfile.yml> prepare`.
// The constructor path, image registry, and OCM binary are forwarded as explicit
// Task variables so the recipe is fully self-contained.
func PrepareOCMComponent(ctx context.Context, name, componentConstructorPath, imageRegistry string) error {
	By("preparing ocm component for " + name)

	absConstructor, err := filepath.Abs(componentConstructorPath)
	if err != nil {
		return fmt.Errorf("could not prepare ocm component %q: resolve constructor path: %w", name, err)
	}
	exampleDir := filepath.Dir(absConstructor)
	taskfile := filepath.Join(exampleDir, "Taskfile.yml")

	cmd := exec.CommandContext(ctx, "task",
		"--taskfile", taskfile,
		"prepare",
		"CONSTRUCTOR="+absConstructor,
		"IMAGE_REGISTRY="+imageRegistry,
		"OCM_CLI="+OCMBinary(),
	)
	cmd.Dir = exampleDir
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("could not prepare ocm component %q: %w", name, err)
	}
	return nil
}

// SigningConfigSignatureNames loads the signing configuration at path and returns the deduplicated list of
// signature names found across all signing.config.ocm.software entries with a non-empty
// signature field. Global entries (no signature field) are skipped: the CLI requires an
// explicit --signature name per invocation.
func SigningConfigSignatureNames(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg genericv1.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse ocmconfig: %w", err)
	}
	entries, err := genericv1.FilterForType[*signingspec.Config](signingspec.Scheme, &cfg)
	if err != nil {
		return nil, fmt.Errorf("filter signing configs: %w", err)
	}
	seen := make(map[string]struct{})
	var names []string
	for _, e := range entries {
		if e.Signature == "" {
			continue
		}
		if _, dup := seen[e.Signature]; !dup {
			seen[e.Signature] = struct{}{}
			names = append(names, e.Signature)
		}
	}
	return names, nil
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

	logCmd := func(label string, args ...string) {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec // args are hardcoded in test code
		output, err := Run(cmd)
		if err != nil {
			logLine(fmt.Sprintf("[DIAG] %s: error: %v", label, err))
		} else {
			for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
				logLine(fmt.Sprintf("[DIAG] %s: %s", label, line))
			}
		}
	}

	logCmd("kro-pods", "kubectl", "get", "pods", "-n", namespace, "-o", "wide")
	logCmd("kro-events", "kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
	logCmd("rgd-conditions",
		"kubectl", "get", resourceType, "-o",
		"custom-columns=NAME:.metadata.name,READY:.status.conditions[?(@.type==\"Ready\")].status,READY_MSG:.status.conditions[?(@.type==\"Ready\")].message",
	)
	logCmd("kro-logs", "kubectl", "logs", "-n", namespace, "--all-containers", "--tail=100", "-l", "app.kubernetes.io/name=kro")
}

// DumpCrossplaneDiagnostics dumps Crossplane composite/managed resource status and
// crossplane-system logs on test failure. DumpLogs targets kro's
// app.kubernetes.io/name=kro selector, which matches nothing in crossplane-system
// (Crossplane pods carry app=crossplane), so the Crossplane flow needs its own dump
// to surface the XR Ready condition, composed resources, and controller logs.
// Creates its own context with a 30s timeout to survive parent context cancellation.
func DumpCrossplaneDiagnostics() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logLine := func(msg string) {
		GinkgoLogr.Info(msg)
	}

	logCmd := func(label string, args ...string) {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec // args are hardcoded in test code
		output, err := Run(cmd)
		if err != nil {
			logLine(fmt.Sprintf("[DIAG] %s: error: %v", label, err))
		} else {
			for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
				logLine(fmt.Sprintf("[DIAG] %s: %s", label, line))
			}
		}
	}

	logCmd("xr-conditions",
		"kubectl", "get", "composite", "-o",
		"custom-columns=NAME:.metadata.name,READY:.status.conditions[?(@.type==\"Ready\")].status,READY_MSG:.status.conditions[?(@.type==\"Ready\")].message",
	)
	// Composed resources the Compositions create. Crossplane v2 has no `managed`
	// category for these, so dump each kind's Ready condition across all namespaces
	// to show which composed resource (e.g. the Flux Kustomization) blocks the XR.
	readyCols := "custom-columns=NS:.metadata.namespace,NAME:.metadata.name," +
		"READY:.status.conditions[?(@.type==\"Ready\")].status," +
		"MSG:.status.conditions[?(@.type==\"Ready\")].message"
	for _, kind := range []string{
		"resources.delivery.ocm.software",
		"gitrepositories.source.toolkit.fluxcd.io",
		"ocirepositories.source.toolkit.fluxcd.io",
		"kustomizations.kustomize.toolkit.fluxcd.io",
		"helmreleases.helm.toolkit.fluxcd.io",
	} {
		logCmd("composed/"+kind, "kubectl", "get", kind, "-A", "-o", readyCols)
	}
	logCmd("composed/applications.argoproj.io",
		"kubectl", "get", "applications.argoproj.io", "-A", "-o",
		"custom-columns=NS:.metadata.namespace,NAME:.metadata.name,"+
			"SYNC:.status.sync.status,HEALTH:.status.health.status",
	)
	logCmd("crossplane-events", "kubectl", "get", "events", "-n", "crossplane-system", "--sort-by=.lastTimestamp")
	logCmd("crossplane-logs", "kubectl", "logs", "-n", "crossplane-system", "--all-containers", "--tail=100", "-l", "app=crossplane")
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
	result, err := GetResourceField(ctx, resource, fieldSelector)
	if err != nil {
		return err
	}

	if result != expected {
		return fmt.Errorf("expected %s, got %s", expected, result)
	}

	return nil
}

// GetResourceField returns the value of a resource field selected by a JSONPath expression.
// Wrapping single quotes emitted by kubectl are stripped.
func GetResourceField(ctx context.Context, resource, fieldSelector string) (string, error) {
	args := []string{"get"}
	args = append(args, strings.Split(resource, " ")...)
	args = append(args, "-o", "jsonpath="+fieldSelector)
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	output, err := Run(cmd)
	if err != nil {
		return "", err
	}

	result := strings.Trim(strings.TrimSpace(string(output)), "'")
	return result, nil
}
