package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // Ginkgo DSL requires dot imports
	. "github.com/onsi/gomega"    //nolint:revive // Ginkgo DSL requires dot imports

	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/kubernetes/controller/test/e2e/hooks"
	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

const e2eYamlFile = "e2e.yaml"

type ScenarioConfig struct {
	// Folder, SimpleName, Dir are not loaded from YAML; they are populated
	// by the loader from the scenario's path on disk.
	Folder     string `json:"-"`
	SimpleName string `json:"-"`
	Dir        string `json:"-"`

	Timeout  string       `json:"timeout,omitempty"`
	Requires []string     `json:"requires,omitempty"`
	Prepare  PrepareSpec  `json:"prepare,omitempty"`
	Deploy   []DeployStep `json:"deploy,omitempty"`
	Assert   AssertSpec   `json:"assert,omitempty"`
	Debug    []DebugCmd   `json:"debug,omitempty"`

	PreDeployHooks   []string `json:"preDeployHooks,omitempty"`
	PostDeployHooks  []string `json:"postDeployHooks,omitempty"`
	PreAssertHooks   []string `json:"preAssertHooks,omitempty"`
	PostAssertHooks  []string `json:"postAssertHooks,omitempty"`
	PreCleanupHooks  []string `json:"preCleanupHooks,omitempty"`
	PostCleanupHooks []string `json:"postCleanupHooks,omitempty"`
}

type DebugCmd struct {
	Kubectl string `json:"kubectl"`
	Label   string `json:"label,omitempty"`
}

type PrepareSpec struct {
	Components []PrepareComponent `json:"components,omitempty"`
}

type PrepareComponent struct {
	Constructor   string `json:"constructor"`
	SigningKey    string `json:"signingKey,omitempty"`
	OCMConfig     string `json:"ocmConfig,omitempty"`
	Registry      string `json:"registry,omitempty"`
	CopyResources bool   `json:"copyResources,omitempty"`
}

type DeployStep struct {
	Apply   string      `json:"apply,omitempty"`
	WaitFor WaitForList `json:"waitFor,omitempty"`
}

type WaitForList []WaitForSpec

func (w *WaitForList) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] != '[' {
		return fmt.Errorf("waitFor must be an array, got: %s", string(data[:min(len(data), 40)]))
	}
	var list []WaitForSpec
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	*w = list
	return nil
}

type WaitForSpec struct {
	Kubectl string `json:"kubectl,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

type AssertSpec struct {
	FieldEquals []FieldEquals `json:"fieldEquals,omitempty"`
}

type FieldEquals struct {
	Resource string `json:"resource"`
	JSONPath string `json:"jsonPath"`
	Value    string `json:"value"`
}

// walkScenarios returns the absolute paths of every directory under root that
// contains an e2e.yaml. The walker stops descending into a directory once it
// finds an e2e.yaml there: nested scenarios are illegal.
//
// A non-existent root returns nil, nil.
func walkScenarios(root string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if _, statErr := os.Stat(filepath.Join(path, e2eYamlFile)); statErr == nil {
			found = append(found, path)
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(found)
	return found, nil
}

func loadScenario(scenarioDir, root, componentsDir string, vars map[string]string) (*ScenarioConfig, error) {
	folder, err := filepath.Rel(root, scenarioDir)
	if err != nil {
		return nil, fmt.Errorf("scenario %q is not under root %q: %w", scenarioDir, root, err)
	}
	folder = filepath.ToSlash(folder)
	simpleName := strings.ReplaceAll(folder, "/", "-")

	merged := make(map[string]string, len(vars)+1)
	for k, v := range vars {
		merged[k] = v
	}
	merged["SCENARIO_SIMPLE_NAME"] = simpleName

	raw, err := os.ReadFile(filepath.Join(scenarioDir, e2eYamlFile))
	if err != nil {
		return nil, fmt.Errorf("read %s/%s: %w", scenarioDir, e2eYamlFile, err)
	}

	substituted, err := substituteVars(string(raw), merged)
	if err != nil {
		return nil, fmt.Errorf("substitute vars in %s/%s: %w", scenarioDir, e2eYamlFile, err)
	}

	var cfg ScenarioConfig
	if err := yaml.Unmarshal([]byte(substituted), &cfg); err != nil {
		return nil, fmt.Errorf("parse %s/%s: %w", scenarioDir, e2eYamlFile, err)
	}
	cfg.Folder = folder
	cfg.SimpleName = simpleName
	cfg.Dir = scenarioDir

	if len(cfg.Prepare.Components) == 0 {
		if _, err := os.Stat(filepath.Join(scenarioDir, "component-constructor.yaml")); err == nil {
			cfg.Prepare.Components = []PrepareComponent{{Constructor: "component-constructor.yaml"}}
		}
	}

	if err := validateHookRefs(&cfg); err != nil {
		return nil, fmt.Errorf("scenario %s: %w", folder, err)
	}

	if componentsDir != "" {
		if err := validateRequires(&cfg, componentsDir); err != nil {
			return nil, fmt.Errorf("scenario %s: %w", folder, err)
		}
	}

	return &cfg, nil
}

func validateRequires(cfg *ScenarioConfig, componentsDir string) error {
	var missing []string
	for _, name := range cfg.Requires {
		script := filepath.Join(componentsDir, name+".sh")
		if _, err := os.Stat(script); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("unknown requires entries (no matching script in %s): %s",
			componentsDir, strings.Join(missing, ", "))
	}
	return nil
}

func validateHookRefs(cfg *ScenarioConfig) error {
	phases := map[string][]string{
		"preDeployHooks":   cfg.PreDeployHooks,
		"postDeployHooks":  cfg.PostDeployHooks,
		"preAssertHooks":   cfg.PreAssertHooks,
		"postAssertHooks":  cfg.PostAssertHooks,
		"preCleanupHooks":  cfg.PreCleanupHooks,
		"postCleanupHooks": cfg.PostCleanupHooks,
	}
	var missing []string
	for phase, names := range phases {
		for _, name := range names {
			if _, ok := hooks.Resolve(name); !ok {
				missing = append(missing, fmt.Sprintf("%s[%q]", phase, name))
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("unknown hook reference(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

// varRef matches ${NAME} — fixed-list envsubst, not bash expansions.
var varRef = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)

func substituteVars(s string, vars map[string]string) (string, error) {
	var unknown []string
	out := varRef.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-1]
		val, ok := vars[name]
		if !ok {
			unknown = append(unknown, name)
			return match
		}
		return val
	})
	if len(unknown) > 0 {
		// Deduplicate while preserving order of first occurrence.
		seen := make(map[string]struct{}, len(unknown))
		var dedup []string
		for _, name := range unknown {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			dedup = append(dedup, name)
		}
		return "", fmt.Errorf("unknown variable(s): %s", strings.Join(dedup, ", "))
	}
	return out, nil
}

const controllerNamespace = "ocm-k8s-toolkit-system"

func builtinVars() map[string]string {
	registry := os.Getenv("IMAGE_REGISTRY")
	host := strings.TrimPrefix(strings.TrimPrefix(registry, "https://"), "http://")
	return map[string]string{
		"IMAGE_REGISTRY_HOST":                   host,
		"CONTROLLER_NAMESPACE":                  controllerNamespace,
		"PROTECTED_REGISTRY_BASIC_AUTH":         os.Getenv("PROTECTED_REGISTRY_URL"),
		"PROTECTED_REGISTRY_DOCKER_CONFIG_JSON": os.Getenv("PROTECTED_REGISTRY_URL2"),
	}
}

func runScenario(ctx SpecContext, cfg *ScenarioConfig) {
	timeout := scenarioTimeout(cfg)
	imageRegistry := os.Getenv("IMAGE_REGISTRY")

	DeferCleanup(func(ctx SpecContext) {
		if !CurrentSpecReport().Failed() && !isWorkflowDebug() {
			return
		}
		runDebugCommands(ctx, cfg)
	})

	scenarioCtx := &hooks.Scenario{
		Folder:     cfg.Folder,
		SimpleName: cfg.SimpleName,
		Dir:        cfg.Dir,
	}

	By("preparing OCM components for " + cfg.Folder)
	for _, comp := range cfg.Prepare.Components {
		signingKey := ""
		if comp.SigningKey != "" {
			signingKey = filepath.Join(cfg.Dir, comp.SigningKey)
		} else if candidate := filepath.Join(cfg.Dir, "ocm.software"); fileExists(candidate) {
			signingKey = candidate
		}
		ocmConfig := ""
		if comp.OCMConfig != "" {
			ocmConfig = filepath.Join(cfg.Dir, comp.OCMConfig)
		}
		registry := imageRegistry
		if comp.Registry != "" {
			registry = comp.Registry
		}
		constructor := comp.Constructor
		if constructor == "" {
			constructor = "component-constructor.yaml"
		}
		Expect(utils.PrepareOCMComponentWithOptions(ctx, utils.PrepareOCMComponentOptions{
			Name:                     cfg.SimpleName,
			ComponentConstructorPath: filepath.Join(cfg.Dir, constructor),
			ImageRegistry:            registry,
			SigningKey:               signingKey,
			OCMConfig:                ocmConfig,
			CopyResources:            comp.CopyResources,
		})).To(Succeed(), "PrepareOCMComponent failed for %s", comp.Constructor)
	}

	dispatchHooks(ctx, "preDeployHooks", cfg.PreDeployHooks, scenarioCtx)

	By("deploying scenario " + cfg.Folder)
	for _, step := range cfg.Deploy {
		if step.Apply != "" {
			manifest := filepath.Join(cfg.Dir, step.Apply)
			err := utils.DeployResource(ctx, manifest)
			Expect(err).NotTo(HaveOccurred(), "kubectl apply -f %s failed", manifest)
		}
		for _, w := range step.WaitFor {
			t := timeout
			if w.Timeout != "" {
				t = w.Timeout
			}
			args := strings.Fields(w.Kubectl)
			args = append(args, "--timeout="+t)
			cmd := exec.CommandContext(ctx, "kubectl", append([]string{"wait"}, args...)...)
			out, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "kubectl wait %s failed: %s", w.Kubectl, string(out))
		}
	}

	dispatchHooks(ctx, "postDeployHooks", cfg.PostDeployHooks, scenarioCtx)
	dispatchHooks(ctx, "preAssertHooks", cfg.PreAssertHooks, scenarioCtx)

	By("asserting scenario " + cfg.Folder)
	for _, fe := range cfg.Assert.FieldEquals {
		Expect(utils.CompareResourceField(ctx, fe.Resource, fe.JSONPath, fe.Value)).To(
			Succeed(),
			"fieldEquals mismatch on %s %s", fe.Resource, fe.JSONPath,
		)
	}

	dispatchHooks(ctx, "postAssertHooks", cfg.PostAssertHooks, scenarioCtx)
	dispatchHooks(ctx, "preCleanupHooks", cfg.PreCleanupHooks, scenarioCtx)
	for _, name := range cfg.PostCleanupHooks {
		hook, ok := hooks.Resolve(name)
		Expect(ok).To(BeTrue(), "unknown postCleanupHooks reference %q", name)
		DeferCleanup(func(ctx SpecContext) {
			Expect(hook(ctx, scenarioCtx)).To(Succeed(), "postCleanupHooks[%q] failed", name)
		})
	}
}

func scenarioTimeout(cfg *ScenarioConfig) string {
	if cfg.Timeout != "" {
		return cfg.Timeout
	}
	if t := os.Getenv("RESOURCE_TIMEOUT"); t != "" {
		return t
	}
	return "10m"
}

func dispatchHooks(ctx context.Context, phase string, names []string, scenario *hooks.Scenario) {
	for _, name := range names {
		hook, ok := hooks.Resolve(name)
		Expect(ok).To(BeTrue(), "%s references unknown hook %q (load-time validation should have caught this)", phase, name)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		err := hook(ctx, scenario)
		Expect(err).NotTo(HaveOccurred(), "%s[%q] failed", phase, name)
	}
}

var defaultDebugCommands = []DebugCmd{
	{Kubectl: "get pods -n " + controllerNamespace + " -o wide", Label: "controller-pods"},
	{Kubectl: "logs -n " + controllerNamespace + " deploy/ocm-k8s-toolkit-controller-manager --tail=80 --all-containers", Label: "controller-logs"},
	{Kubectl: "get pods -n kro -o wide", Label: "kro-pods"},
	{Kubectl: "get events --sort-by=.lastTimestamp", Label: "events"},
	{Kubectl: "get rgd -o custom-columns=NAME:.metadata.name,READY:.status.conditions[?(@.type==\"Ready\")].status,READY_MSG:.status.conditions[?(@.type==\"Ready\")].message", Label: "rgd-conditions"},
}

func isWorkflowDebug() bool {
	return os.Getenv("RUNNER_DEBUG") == "1" ||
		strings.EqualFold(os.Getenv("ACTIONS_STEP_DEBUG"), "true")
}

func runDebugCommands(ctx context.Context, cfg *ScenarioConfig) {
	cmds := cfg.Debug
	if len(cmds) == 0 {
		cmds = defaultDebugCommands
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, d := range cmds {
		label := d.Label
		if label == "" {
			label = d.Kubectl
		}
		args := append([]string{}, strings.Fields(d.Kubectl)...)
		cmd := exec.CommandContext(ctx, "kubectl", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			GinkgoLogr.Info(fmt.Sprintf("[DEBUG] %s: error: %v", label, err))
		} else {
			for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
				GinkgoLogr.Info(fmt.Sprintf("[DEBUG] %s: %s", label, line))
			}
		}
	}
}

// fileExists reports whether path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func componentsDir() string {
	if dir := os.Getenv("SETUP_COMPONENTS_DIR"); dir != "" {
		return dir
	}
	base := os.Getenv("PROJECT_DIR")
	if base == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return ""
		}
		base = cwd
	}
	return filepath.Join(base, "test", "e2e", "setup", "components")
}

// scenarioMatchesFocus reports whether a scenario's folder matches any of the
// Ginkgo focus patterns. An empty patterns list means "match everything".
func scenarioMatchesFocus(folder string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	specName := "should run " + folder
	for _, pat := range patterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			continue
		}
		if re.MatchString(specName) {
			return true
		}
	}
	return false
}
