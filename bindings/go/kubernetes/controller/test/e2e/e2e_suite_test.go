package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/test/utils"
)

const namespace = "ocm-k8s-toolkit-system"

var (
	// imageRegistry is the OCI registry the suite pushes OCM components to.
	imageRegistry string
	// timeout is the default kubectl wait timeout for all e2e assertions.
	timeout string
	// controllerPodName is captured in BeforeSuite for log collection.
	controllerPodName string
	// examplesDir is the root of the examples tree, resolved once at suite
	// startup. EXAMPLES_DIR overrides it; PROJECT_DIR/<examples> is the
	// second option; relative-to-this-file is the fallback.
	examplesDir = defaultExamplesDir()
)

func defaultExamplesDir() string {
	if dir := os.Getenv("EXAMPLES_DIR"); dir != "" {
		return dir
	}
	if projectDir := os.Getenv("PROJECT_DIR"); projectDir != "" {
		return filepath.Join(projectDir, "examples")
	}
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "examples")
}

// Run e2e tests using the Ginkgo runner.
// The suite needs a provisioned kind cluster (task test/e2e/fresh). It is
// excluded from the module-wide unit sweep in bindings/go/Taskfile.yml, the
// same way */integration packages are.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	fmt.Fprintf(GinkgoWriter, "Starting ocm-k8s-toolkit suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
	timeout = os.Getenv("RESOURCE_TIMEOUT")
	if timeout == "" {
		timeout = "10m"
	}

	imageRegistry = os.Getenv("IMAGE_REGISTRY")
	Expect(imageRegistry).NotTo(BeEmpty(), "IMAGE_REGISTRY must be set")

	By("Starting the operator", func() {
		By("Validating that the controller-manager pod is running as expected")
		verifyControllerUp := func(ctx context.Context) error {
			// Get pod name

			cmd := exec.CommandContext(ctx, "kubectl", "get",
				"pods", "-l", "control-plane=controller-manager",
				"-o", "go-template={{ range .items }}"+
					"{{ if not .metadata.deletionTimestamp }}"+
					"{{ .metadata.name }}"+
					"{{ \"\\n\" }}{{ end }}{{ end }}",
				"-n", namespace,
			)

			podOutput, err := utils.Run(cmd)
			ExpectWithOffset(2, err).NotTo(HaveOccurred())

			var podNames []string
			podNamesDirty := strings.Split(string(podOutput), "\n")
			for _, podName := range podNamesDirty {
				if podName != "" {
					podNames = append(podNames, podName)
				}
			}
			if len(podNames) != 1 {
				return fmt.Errorf("expect 1 controller pods running, but got %d", len(podNames))
			}
			controllerPodName = podNames[0]
			ExpectWithOffset(2, controllerPodName).Should(ContainSubstring("controller-manager"))

			// Validate pod status
			cmd = exec.CommandContext(ctx, "kubectl", "get",
				"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
				"-n", namespace,
			)
			status, err := utils.Run(cmd)
			ExpectWithOffset(2, err).NotTo(HaveOccurred())
			if string(status) != "Running" {
				return fmt.Errorf("controller pod in %s status", status)
			}
			return nil
		}

		EventuallyWithOffset(1, verifyControllerUp, time.Minute, time.Second).WithContext(ctx).Should(Succeed())
	})
})

var _ = AfterSuite(func(ctx SpecContext) {
	logPath := os.Getenv("CONTROLLER_LOG_PATH")
	if logPath != "" {
		By("displays logs from the controller", func() {
			cmdArgs := []string{
				"logs",
				"-n",
				namespace,
				controllerPodName,
				"--log-path",
				os.Getenv("CONTROLLER_LOG_PATH"),
			}
			cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
			_, err := utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		})
	}
})
