package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubstituteVars(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		vars    map[string]string
		want    string
		wantErr string
	}{
		{
			name: "no variables",
			in:   "plain text with no refs",
			vars: nil,
			want: "plain text with no refs",
		},
		{
			name: "single substitution",
			in:   "hello ${NAME}",
			vars: map[string]string{"NAME": "world"},
			want: "hello world",
		},
		{
			name: "multiple substitutions",
			in:   "${A}-${B}-${A}",
			vars: map[string]string{"A": "x", "B": "y"},
			want: "x-y-x",
		},
		{
			name:    "unknown variable",
			in:      "hello ${UNKNOWN}",
			vars:    map[string]string{"NAME": "world"},
			wantErr: "UNKNOWN",
		},
		{
			name:    "duplicate unknown is reported once",
			in:      "${X} and ${X} again",
			vars:    nil,
			wantErr: "X",
		},
		{
			name:    "multiple unknowns are reported together",
			in:      "${A} ${B}",
			vars:    nil,
			wantErr: "A, B",
		},
		{
			name: "shell default syntax is not interpreted",
			in:   "${X:-default}",
			vars: nil,
			want: "${X:-default}",
		},
		{
			name: "lowercase names are not matched",
			in:   "${name}",
			vars: nil,
			want: "${name}",
		},
		{
			name: "scenario name embedding",
			in:   "deployment.apps/${SCENARIO_SIMPLE_NAME}-podinfo",
			vars: map[string]string{"SCENARIO_SIMPLE_NAME": "helm-simple"},
			want: "deployment.apps/helm-simple-podinfo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			got, err := substituteVars(tt.in, tt.vars)
			if tt.wantErr != "" {
				r.Error(err)
				r.Contains(err.Error(), tt.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tt.want, got)
		})
	}
}

func TestWalkScenarios(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	root := t.TempDir()

	mkdir := func(p string) {
		t.Helper()
		r.NoError(os.MkdirAll(filepath.Join(root, p), 0o755))
	}
	touch := func(p string) {
		t.Helper()
		r.NoError(os.WriteFile(filepath.Join(root, p), []byte{}, 0o644))
	}
	mkdir("helm/simple/nested")
	mkdir("helm/signing")
	mkdir("kustomize/simple")
	touch("helm/simple/e2e.yaml")
	touch("helm/simple/nested/e2e.yaml")
	touch("helm/signing/e2e.yaml")
	touch("README.md")

	got, err := walkScenarios(root)
	r.NoError(err)

	want := []string{
		filepath.Join(root, "helm", "signing"),
		filepath.Join(root, "helm", "simple"),
	}
	r.Equal(want, got)
}

func TestWalkScenariosMissingRoot(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	got, err := walkScenarios(filepath.Join(t.TempDir(), "does-not-exist"))
	r.NoError(err)
	r.Empty(got)
}

func TestLoadScenarioMinimal(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	root := t.TempDir()
	scenarioDir := filepath.Join(root, "helm", "simple")
	r.NoError(os.MkdirAll(scenarioDir, 0o755))
	yaml := `
requires:
  - kro
  - flux-source

prepare:
  components:
    - constructor: component-constructor.yaml

deploy:
  - apply: bootstrap.yaml
  - apply: rgd.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Ready=true rgd/${SCENARIO_SIMPLE_NAME}"
`
	r.NoError(os.WriteFile(filepath.Join(scenarioDir, "e2e.yaml"), []byte(yaml), 0o644))

	cfg, err := loadScenario(scenarioDir, root, "", nil)
	r.NoError(err)
	r.Equal("helm/simple", cfg.Folder)
	r.Equal("helm-simple", cfg.SimpleName)
	r.Equal(scenarioDir, cfg.Dir)
	r.Equal([]string{"kro", "flux-source"}, cfg.Requires)
	r.Len(cfg.Deploy, 2)
	r.NotEmpty(cfg.Deploy[1].WaitFor)
	r.Equal("--for=create --for=condition=Ready=true rgd/helm-simple", cfg.Deploy[1].WaitFor[0].Kubectl)
}

func TestLoadScenarioUnknownHookRejected(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	root := t.TempDir()
	scenarioDir := filepath.Join(root, "applyset", "pruning")
	r.NoError(os.MkdirAll(scenarioDir, 0o755))
	yaml := `
requires: []
postAssertHooks:
  - thisHookDoesNotExist
deploy:
  - apply: bootstrap.yaml
assert: {}
`
	r.NoError(os.WriteFile(filepath.Join(scenarioDir, "e2e.yaml"), []byte(yaml), 0o644))

	_, err := loadScenario(scenarioDir, root, "", nil)
	r.Error(err)
	r.Contains(err.Error(), "thisHookDoesNotExist")
}

func TestLoadScenarioUnknownVarRejected(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	root := t.TempDir()
	scenarioDir := filepath.Join(root, "helm", "simple")
	r.NoError(os.MkdirAll(scenarioDir, 0o755))
	yaml := `
deploy:
  - apply: ${MADE_UP_VARIABLE}
`
	r.NoError(os.WriteFile(filepath.Join(scenarioDir, "e2e.yaml"), []byte(yaml), 0o644))

	_, err := loadScenario(scenarioDir, root, "", nil)
	r.Error(err)
	r.Contains(err.Error(), "MADE_UP_VARIABLE")
}

func TestLoadScenarioUnknownRequiresRejected(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	root := t.TempDir()
	scenarioDir := filepath.Join(root, "helm", "simple")
	r.NoError(os.MkdirAll(scenarioDir, 0o755))
	yaml := `
requires: [kro, this-component-does-not-exist]
deploy: []
`
	r.NoError(os.WriteFile(filepath.Join(scenarioDir, "e2e.yaml"), []byte(yaml), 0o644))

	componentsDir := t.TempDir()
	r.NoError(os.WriteFile(filepath.Join(componentsDir, "kro.sh"), []byte("#!/usr/bin/env bash\n"), 0o755))

	_, err := loadScenario(scenarioDir, root, componentsDir, nil)
	r.Error(err)
	r.Contains(err.Error(), "this-component-does-not-exist")
}

func TestLoadScenarioRequiresAllPresent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	root := t.TempDir()
	scenarioDir := filepath.Join(root, "helm", "simple")
	r.NoError(os.MkdirAll(scenarioDir, 0o755))
	yaml := `
requires: [kro, flux-source]
deploy: []
`
	r.NoError(os.WriteFile(filepath.Join(scenarioDir, "e2e.yaml"), []byte(yaml), 0o644))

	componentsDir := t.TempDir()
	for _, n := range []string{"kro.sh", "flux-source.sh"} {
		r.NoError(os.WriteFile(filepath.Join(componentsDir, n), []byte("#!/usr/bin/env bash\n"), 0o755))
	}

	_, err := loadScenario(scenarioDir, root, componentsDir, nil)
	r.NoError(err)
}

func TestBuiltinVarsRegistryHostStripsScheme(t *testing.T) {
	// Not parallel: sub-tests use t.Setenv which requires sequential execution.
	tests := []struct {
		name     string
		registry string
		wantHost string
	}{
		{name: "http scheme stripped", registry: "http://image-registry:5000", wantHost: "image-registry:5000"},
		{name: "https scheme stripped", registry: "https://image-registry:5000", wantHost: "image-registry:5000"},
		{name: "no scheme unchanged", registry: "image-registry:5000", wantHost: "image-registry:5000"},
		{name: "empty registry", registry: "", wantHost: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			t.Setenv("IMAGE_REGISTRY", tt.registry)
			got := builtinVars()
			r.Equal(tt.wantHost, got["IMAGE_REGISTRY_HOST"])
		})
	}
}

func TestIsWorkflowDebug(t *testing.T) {
	// Not parallel: sub-tests use t.Setenv which requires sequential execution.
	tests := []struct {
		name      string
		runner    string
		stepDebug string
		want      bool
	}{
		{name: "neither set", want: false},
		{name: "RUNNER_DEBUG=1", runner: "1", want: true},
		{name: "RUNNER_DEBUG=0 ignored", runner: "0", want: false},
		{name: "RUNNER_DEBUG=true ignored", runner: "true", want: false},
		{name: "ACTIONS_STEP_DEBUG=true", stepDebug: "true", want: true},
		{name: "ACTIONS_STEP_DEBUG=TRUE", stepDebug: "TRUE", want: true},
		{name: "ACTIONS_STEP_DEBUG=false", stepDebug: "false", want: false},
		{name: "ACTIONS_STEP_DEBUG=1 ignored", stepDebug: "1", want: false},
		{name: "both set", runner: "1", stepDebug: "true", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			t.Setenv("RUNNER_DEBUG", tt.runner)
			t.Setenv("ACTIONS_STEP_DEBUG", tt.stepDebug)
			r.Equal(tt.want, isWorkflowDebug())
		})
	}
}
