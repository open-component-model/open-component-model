// Hooks that drive the applyset-pruning scenario's multi-stage choreography.
// The flow they implement, in order:
//
//   1. applysetPatchToV2  — bump the Component CR's spec.semver to 2.0.0,
//      then wait until the controller observes status.component.version=2.0.0.
//   2. applysetAssertPruning — verify the v1-only deployment (podinfo-2) is
//      pruned while the deployment present in both versions (podinfo) stays.
//   3. applysetDeleteDeployer — delete the Deployer resource and wait for
//      the deletion to complete.
//   4. applysetAssertCascade — confirm the deployer's owned objects (the
//      remaining podinfo deployment) are also cleaned up.
//
// These mirror the imperative steps the legacy e2e_applyset_test.go used to
// run inline. They live behind named registry entries because the harness
// schema is final-state declarative; this kind of mid-flight CR mutation
// does not belong in e2e.yaml.

package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

// hookTimeout returns the timeout to pass to kubectl wait calls inside hooks.
// Uses RESOURCE_TIMEOUT (already used by the runner's scenarioTimeout) when
// set, falling back to a conservative default.
func hookTimeout() string {
	if t := os.Getenv("RESOURCE_TIMEOUT"); t != "" {
		return t
	}
	return "5m"
}

// componentName builds the Component CR target string used by kubectl. The
// scenario's bootstrap.yaml pins the component metadata.name to
// "<simple>-component"; the legacy test relied on this naming convention and
// we keep it.
func componentName(s *Scenario) string {
	return "component.delivery.ocm.software/" + s.SimpleName + "-component"
}

// deployerName mirrors componentName for the Deployer CR.
func deployerName(s *Scenario) string {
	return "deployer.delivery.ocm.software/" + s.SimpleName + "-deployer"
}

// applysetPatchToV2 bumps the Component spec to v2.0.0 and waits for the
// controller to reconcile status.component.version to "2.0.0". The wait is
// implemented as a polling loop because `kubectl wait` cannot match against
// jsonpath equality on every controller version we care about.
func applysetPatchToV2(ctx context.Context, s *Scenario) error {
	component := componentName(s)
	patch := exec.CommandContext(ctx, "kubectl", "patch",
		component,
		"--type", "merge",
		"-p", `{"spec":{"semver":"2.0.0"}}`,
		"-n", "default",
	)
	if _, err := utils.Run(patch); err != nil {
		return fmt.Errorf("patch component to 2.0.0: %w", err)
	}

	pollCtx, cancel := context.WithDeadline(ctx, time.Now().Add(parseTimeout(hookTimeout())))
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		get := exec.CommandContext(pollCtx,
			"kubectl", "get", component,
			"-n", "default",
			"-o", "jsonpath={.status.component.version}",
		)
		out, err := utils.Run(get)
		if err == nil && strings.TrimSpace(string(out)) == "2.0.0" {
			break
		}
		select {
		case <-pollCtx.Done():
			return fmt.Errorf("component version did not reach 2.0.0 within timeout: %w", pollCtx.Err())
		case <-ticker.C:
		}
	}

	resource := "resource.delivery.ocm.software/" + s.SimpleName + "-resource"
	if err := utils.WaitForResource(ctx, "condition=Ready=true", hookTimeout(), resource); err != nil {
		return fmt.Errorf("wait resource Ready=true after v2 patch: %w", err)
	}
	return nil
}

// applysetAssertPruning verifies that podinfo-2 (only present in v1) has
// been removed and that podinfo (present in both) remains Available.
func applysetAssertPruning(ctx context.Context, s *Scenario) error {
	if err := waitForPodinfo2Pruned(ctx); err != nil {
		return err
	}
	deployment := "deployment.apps/" + s.SimpleName + "-podinfo"
	if err := utils.WaitForResource(ctx, "condition=Available", hookTimeout(), deployment); err != nil {
		return fmt.Errorf("podinfo deployment lost Available after prune: %w", err)
	}
	return nil
}

func waitForPodinfo2Pruned(ctx context.Context) error {
	pollCtx, cancel := context.WithDeadline(ctx, time.Now().Add(parseTimeout("1m")))
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		cmd := exec.CommandContext(pollCtx,
			"kubectl", "get", "deployments",
			"-n", "default",
			"-l", "app=podinfo-2",
			"-o", "json",
		)
		out, err := utils.Run(cmd)
		if err == nil {
			var result map[string]any
			if jsonErr := json.Unmarshal(out, &result); jsonErr == nil {
				if items, ok := result["items"].([]any); ok && len(items) == 0 {
					return nil
				}
			}
		}
		select {
		case <-pollCtx.Done():
			return fmt.Errorf("podinfo-2 deployment was not pruned within timeout: %w", pollCtx.Err())
		case <-ticker.C:
		}
	}
}

// applysetDeleteDeployer deletes the Deployer CR and confirms it is gone.
func applysetDeleteDeployer(ctx context.Context, s *Scenario) error {
	deployer := deployerName(s)
	if err := utils.DeleteResource(ctx, hookTimeout(), deployer); err != nil {
		return fmt.Errorf("delete deployer: %w", err)
	}

	pollCtx, cancel := context.WithDeadline(ctx, time.Now().Add(parseTimeout(hookTimeout())))
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		cmd := exec.CommandContext(pollCtx, "kubectl", "get", deployer, "-n", "default")
		if _, err := utils.Run(cmd); err != nil {
			return nil
		}
		select {
		case <-pollCtx.Done():
			return fmt.Errorf("deployer %s still present after timeout: %w", deployer, pollCtx.Err())
		case <-ticker.C:
		}
	}
}

// applysetAssertCascade asserts that deleting the Deployer also tore down
// the remaining podinfo deployment it owned.
func applysetAssertCascade(ctx context.Context, s *Scenario) error {
	deployment := "deployment.apps/" + s.SimpleName + "-podinfo"
	pollCtx, cancel := context.WithDeadline(ctx, time.Now().Add(parseTimeout(hookTimeout())))
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		cmd := exec.CommandContext(pollCtx, "kubectl", "get", deployment, "-n", "default")
		if _, err := utils.Run(cmd); err != nil {
			return nil
		}
		select {
		case <-pollCtx.Done():
			return fmt.Errorf("deployment %s still present after deployer delete: %w", deployment, pollCtx.Err())
		case <-ticker.C:
		}
	}
}

// parseTimeout coerces the kubectl-style duration strings used by the
// runner ("5m", "30s") into a time.Duration. Bad input falls back to 5m so a
// scenario typo cannot wedge a hook indefinitely.
func parseTimeout(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}
