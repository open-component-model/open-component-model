package utils

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	ocistore "oras.land/oras-go/v2/content/oci"
	"sigs.k8s.io/yaml"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	signingspec "ocm.software/open-component-model/bindings/go/signing/v1alpha1/spec"
)

const (
	componentNamePrefix = "ocm.software/ocm-k8s-toolkit/examples/"
	// signingVersion is the version stamped on every signed fixture today. Sign
	// requires an exact name:version reference; transfer does not.
	signingVersion = "1.0.0"
)

// OCMBinary returns the ocm CLI executable. Override via OCM_CLI when running
// against a non-standard binary path.
func OCMBinary() string {
	if v := os.Getenv("OCM_CLI"); v != "" {
		return v
	}
	return "ocm"
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
//
// When the example directory ships an .ocmconfig alongside the component-constructor.yaml,
// PrepareOCMComponent signs the component with every signature configured in that file
// (one `ocm sign cv` invocation per signing.config.ocm.software entry with a non-empty
// signature name) and creates a Kubernetes Secret named <name>-ocmconfig in the default
// namespace carrying the same file. The controller reads verification credentials from
// that Secret via spec.ocmConfig on the Component CR. The Secret is deleted after the
// test through DeferCleanup.
func PrepareOCMComponent(ctx context.Context, name, componentConstructorPath, imageRegistry string) error {
	ocm := OCMBinary()

	By("creating ocm component for " + name)
	tmpDir := GinkgoT().TempDir()
	ctfDir := filepath.Join(tmpDir, "ctf")

	exampleDir := filepath.Dir(componentConstructorPath)
	if _, err := os.Stat(filepath.Join(exampleDir, "kustomize")); err == nil {
		if err := buildKustomizeOCILayout(ctx, exampleDir); err != nil {
			return fmt.Errorf("could not build kustomize oci layout: %w", err)
		}
	}

	cmd := exec.CommandContext(ctx, ocm,
		"add", "cv",
		"--repository", ctfDir,
		"--constructor", componentConstructorPath,
	)
	// OCM's dir/v1 input resolves paths relative to the process CWD (not the
	// constructor's working directory), so run add cv from the example dir.
	cmd.Dir = exampleDir
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("could not create ocm component: %w", err)
	}

	componentName := componentNamePrefix + filepath.Base(filepath.Dir(componentConstructorPath))
	transferRef := fmt.Sprintf("ctf::%s//%s", ctfDir, componentName)

	// If the example ships its own .ocmconfig, use it to sign each configured signature
	// and materialize a Kubernetes Secret carrying the same file for the controller.
	ocmConfigPath := filepath.Join(exampleDir, ".ocmconfig")
	if _, err := os.Stat(ocmConfigPath); err == nil {
		sigNames, err := SigningConfigSignatureNames(ocmConfigPath)
		if err != nil {
			return fmt.Errorf("could not read signature names from .ocmconfig: %w", err)
		}
		for _, sigName := range sigNames {
			By(fmt.Sprintf("signing ocm component for %s with signature %q", name, sigName))
			signRef := fmt.Sprintf("ctf::%s//%s:%s", ctfDir, componentName, signingVersion)
			cmd = exec.CommandContext(ctx, ocm,
				"sign", "cv",
				signRef,
				"--signature", sigName,
				"--config", ocmConfigPath,
			)
			cmd.Dir = exampleDir
			if _, err := Run(cmd); err != nil {
				return fmt.Errorf("could not sign ocm component with signature %q: %w", sigName, err)
			}
		}

		By("creating k8s Secret " + name + "-ocmconfig from .ocmconfig")
		secretName := name + "-ocmconfig"
		cmd = exec.CommandContext(ctx, "kubectl", "create", "secret", "generic", secretName,
			"--namespace", "default",
			"--from-file=.ocmconfig="+ocmConfigPath,
		)
		if _, err := Run(cmd); err != nil {
			return fmt.Errorf("could not create ocmconfig secret: %w", err)
		}
		DeferCleanup(func() {
			_, _ = Run(exec.CommandContext(ctx, "kubectl", "delete", "secret", secretName,
				"--namespace", "default", "--ignore-not-found"))
		})
	}

	By("transferring ocm component for " + name)
	cmd = exec.CommandContext(ctx, ocm, "transfer", "cv", transferRef, imageRegistry)

	if strings.Contains(name, "nested") {
		cmd.Args = append(cmd.Args, "--recursive")
	}

	if strings.Contains(name, "localization") {
		cmd.Args = append(cmd.Args, "--copy-resources", "--upload-as", "ociArtifact")
	}

	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("could not transfer ocm component: %w", err)
	}

	return nil
}

// buildKustomizeOCILayout packages exampleDir/kustomize into an OCI image
// layout at exampleDir/oci-layout so the constructor's dir/v1 input can embed
// it as a native OCI manifest. The layer carries the flux content media type
// so a Flux OCIRepository can consume the artifact after transfer.
func buildKustomizeOCILayout(ctx context.Context, exampleDir string) error {
	layoutDir := filepath.Join(exampleDir, "oci-layout")
	if err := os.RemoveAll(layoutDir); err != nil {
		return fmt.Errorf("clean layout dir: %w", err)
	}
	if err := os.MkdirAll(layoutDir, 0o755); err != nil {
		return fmt.Errorf("create layout dir: %w", err)
	}

	store, err := ocistore.New(layoutDir)
	if err != nil {
		return fmt.Errorf("open oci store: %w", err)
	}

	var buf bytes.Buffer
	if err := tarGzipDir(&buf, filepath.Join(exampleDir, "kustomize")); err != nil {
		return fmt.Errorf("tar kustomize: %w", err)
	}

	layerDesc := ocispec.Descriptor{
		MediaType: "application/vnd.cncf.flux.content.v1.tar+gzip",
		Digest:    digest.FromBytes(buf.Bytes()),
		Size:      int64(buf.Len()),
	}
	if err := store.Push(ctx, layerDesc, bytes.NewReader(buf.Bytes())); err != nil {
		return fmt.Errorf("push layer: %w", err)
	}

	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1,
		"application/vnd.cncf.flux.config.v1+json",
		oras.PackManifestOptions{Layers: []ocispec.Descriptor{layerDesc}},
	)
	if err != nil {
		return fmt.Errorf("pack manifest: %w", err)
	}
	if err := store.Tag(ctx, manifestDesc, "latest"); err != nil {
		return fmt.Errorf("tag manifest: %w", err)
	}
	return nil
}

// tarGzipDir writes a gzip'd tar of dir's contents (paths relative to dir) to w.
// Uses os.Root to avoid symlink TOCTOU.
func tarGzipDir(w io.Writer, dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)
	walkErr := fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(path)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		f, err := root.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if cerr := tw.Close(); walkErr == nil {
		walkErr = cerr
	}
	if cerr := gw.Close(); walkErr == nil {
		walkErr = cerr
	}
	return walkErr
}

// SigningConfigSignatureNames loads the .ocmconfig at path and returns the deduplicated list of
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
