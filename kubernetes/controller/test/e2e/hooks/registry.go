// Package hooks holds named imperative escape hatches that scenarios can
// reference from their e2e.yaml. The runner resolves names against Registry
// at suite start; an unknown name is a load-time error.
package hooks

import "context"

// HookFunc is the signature of a scenario hook.
type HookFunc func(ctx context.Context, scenario *Scenario) error

// Scenario is the per-scenario context handed to each hook. It is defined in
// the hooks package so hooks can take a stable dependency on it without
// importing the e2e package (which would create a cycle).
type Scenario struct {
	// Folder is the slash-separated scenario name relative to its root, e.g.
	// "helm/simple" or "credentials/basic-auth".
	Folder string
	// SimpleName is Folder with "/" replaced by "-", safe for embedding in
	// Kubernetes resource names.
	SimpleName string
	// Dir is the absolute path to the scenario folder.
	Dir string
}

var Registry = map[string]HookFunc{
	"applysetPatchToV2":      applysetPatchToV2,
	"applysetAssertPruning":  applysetAssertPruning,
	"applysetDeleteDeployer": applysetDeleteDeployer,
	"applysetAssertCascade":  applysetAssertCascade,
}

func Resolve(name string) (HookFunc, bool) {
	fn, ok := Registry[name]
	return fn, ok
}
