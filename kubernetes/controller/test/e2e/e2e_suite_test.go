package e2e

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

const namespace = "ocm-k8s-toolkit-system"

var (
	// image registry that is used to push and pull images.
	imageRegistry string
	// timeout for waiting for kuberentes resources
	timeout string
	// controllerPodName is required to access the logs after the e2e tests
	controllerPodName string
	examplesDir       string
	examples          []os.DirEntry
)

// To create a test-case for every example in the examples directory, it is required to set the examples before the
// test suite is started.
func init() {
	examplesDir = os.Getenv("EXAMPLES_DIR")
	if examplesDir == "" {
		projectDir := os.Getenv("PROJECT_DIR")
		if projectDir == "" {
			var err error
			projectDir, err = os.Getwd()
			if err != nil {
				log.Fatal("could not get current working directory", err)
			}
		}
		examplesDir = filepath.Join(projectDir, "examples")
	}

	var err error
	examples, err = os.ReadDir(examplesDir)
	if err != nil {
		log.Fatal("could not read directory with examples", err)
	}
}

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	fmt.Fprintf(GinkgoWriter, "Starting ocm-k8s-toolkit suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
	By("Creating manager namespace", func() {
		err := utils.CreateNamespace(namespace)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		DeferCleanup(func() error {
			utils.DeleteNamespace(namespace)
			return nil
		})
	})

	timeout = os.Getenv("RESOURCE_TIMEOUT")
	if timeout == "" {
		timeout = "10m"
	}

	imageRegistry = os.Getenv("IMAGE_REGISTRY")
	Expect(imageRegistry).NotTo(BeEmpty(), "IMAGE_REGISTRY must be set")

	By("Starting the operator", func() {
		// projectimage stores the name of the image used in the example
		var projectimage = strings.TrimLeft(imageRegistry, "http://") + "/ocm.software/ocm-controller:v0.0.1"

		By("Building the manager(Operator) image")
		cmd := exec.Command("make", "docker-build")
		cmd.Env = []string{"IMG=" + projectimage}
		_, err := utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())

		cmd = exec.Command("make", "docker-push")
		cmd.Env = []string{"IMG=" + projectimage}
		_, err = utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())

		By("Installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		DeferCleanup(func() error {
			By("uninstalling CRDs")
			cmd = exec.Command("make", "uninstall")
			// In case ´make undeploy` already uninstalled the CRDs
			cmd.Env = append(os.Environ(), "IGNORE_NOT_FOUND=true")
			_, err = utils.Run(cmd)

			return err
		})

		By("Deploying the controller-manager")
		cmd = exec.Command("make", "deploy")
		cmd.Env = []string{"IMG=" + projectimage}
		_, err = utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		DeferCleanup(func() error {
			By("Un-deploying the controller-manager")
			cmd = exec.Command("make", "undeploy")
			cmd.Env = []string{"IMG=" + projectimage}
			_, err = utils.Run(cmd)

			return err
		})

		By("Validating that the controller-manager pod is running as expected")
		verifyControllerUp := func() error {
			// Get pod name

			cmd = exec.Command("kubectl", "get",
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
			cmd = exec.Command("kubectl", "get",
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

		EventuallyWithOffset(1, verifyControllerUp, time.Minute, time.Second).Should(Succeed())
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
			cmd := exec.Command("kubectl", cmdArgs...)
			_, err := utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		})
	}
})
