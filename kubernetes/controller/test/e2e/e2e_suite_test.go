package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

// controllerPodName is captured by proc 1 during BeforeSuite to verify the controller is running.
var controllerPodName string

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, err := fmt.Fprintf(GinkgoWriter, "Starting ocm-k8s-toolkit suite\n")
	if err != nil {
		Fail("failed to write to GinkgoWriter: " + err.Error())
	}
	RunSpecs(t, "e2e suite")
}

var _ = SynchronizedBeforeSuite(
	// Proc 1 only: install all required components for the active scenarios.
	// Workers read IMAGE_REGISTRY and RESOURCE_TIMEOUT from the environment
	// directly (via builtinVars / scenarioTimeout), so no data needs to be
	// broadcast.
	func(ctx SpecContext) []byte {
		Expect(os.Getenv("IMAGE_REGISTRY")).NotTo(BeEmpty(), "IMAGE_REGISTRY must be set")

		By("installing required components (proc 1)", func() {
			collectAndInstallRequires(ctx)
		})

		By("validating controller-manager is running", func() {
			verifyControllerUp := func(ctx context.Context) error {
				GinkgoHelper()
				cmd := exec.CommandContext(ctx, "kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", controllerNamespace,
				)
				podOutput, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())

				var podNames []string
				for _, name := range strings.Split(string(podOutput), "\n") {
					if name != "" {
						podNames = append(podNames, name)
					}
				}
				if len(podNames) != 1 {
					return fmt.Errorf("expect 1 controller pod running, but got %d", len(podNames))
				}
				controllerPodName = podNames[0]
				Expect(controllerPodName).Should(ContainSubstring("controller-manager"))

				cmd = exec.CommandContext(ctx, "kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", controllerNamespace,
				)
				phase, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())
				if string(phase) != "Running" {
					return fmt.Errorf("controller pod in %s status", phase)
				}
				return nil
			}
			Eventually(verifyControllerUp).WithTimeout(time.Minute).WithPolling(time.Second).WithContext(ctx).Should(Succeed())
		})

		return nil
	},
	// All procs: no shared data to unpack.
	func(_ SpecContext, _ []byte) {},
)

var _ = SynchronizedAfterSuite(
	// All procs: no-op — per-spec cleanup is handled by DeferCleanup inside DeployResource.
	func() {},
	// Proc 1 only: no-op.
	func(_ SpecContext) {},
)

// collectAndInstallRequires walks both scenario roots, filters by the active
// Ginkgo focus filter, unions the requires: entries across all matching
// scenarios, and runs each component's install script exactly once.
// This runs only on proc 1 inside SynchronizedBeforeSuite.
func collectAndInstallRequires(ctx SpecContext) {
	suiteConf, _ := GinkgoConfiguration()
	focusPatterns := suiteConf.FocusStrings

	dir := projectDir()
	roots := []string{
		filepath.Join(dir, "examples"),
		filepath.Join(dir, "test", "e2e", "scenarios"),
	}

	compsDir := componentsDir()
	vars := builtinVars()

	seen := make(map[string]struct{})
	var ordered []string

	for _, root := range roots {
		dirs, err := walkScenarios(root)
		if err != nil {
			continue
		}
		for _, scenDir := range dirs {
			cfg, err := loadScenario(scenDir, root, compsDir, vars)
			if err != nil {
				continue
			}
			if !scenarioMatchesFocus(cfg.Folder, focusPatterns) {
				continue
			}
			for _, name := range cfg.Requires {
				if _, ok := seen[name]; !ok {
					seen[name] = struct{}{}
					ordered = append(ordered, name)
				}
			}
		}
	}

	g, gctx := errgroup.WithContext(ctx)
	for _, name := range ordered {
		g.Go(func() error {
			script := filepath.Join(compsDir, name+".sh")
			var buf bytes.Buffer
			cmd := exec.CommandContext(gctx, "bash", script)
			cmd.Stdout = &buf
			cmd.Stderr = &buf
			err := cmd.Run()
			if err != nil {
				return fmt.Errorf("component script %s failed: %w", script, err)
			}
			_, err = fmt.Fprintf(GinkgoWriter, "\n------------------------------------------\n==> component %q\n%s", name, buf.String())
			if err != nil {
				Fail("failed to write to GinkgoWriter: " + err.Error())
			}

			return nil
		})
	}
	Expect(g.Wait()).To(Succeed())
}
