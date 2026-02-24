package e2e

import (
	"fmt"
	"os"
	"testing"
)

var (
	// Global TestEnv
	env *TestEnv
)

// TestEnv holds the configuration for the test execution.
type TestEnv struct {
	Config *Config
}

// TestMain acts as the global entry point for the test suite.
func TestMain(m *testing.M) {
	// Parse Flags
	cfg := ParseConfig()

	// Initialize TestEnv
	env = &TestEnv{
		Config: cfg,
	}

	// Run Tests
	code := m.Run()

	os.Exit(code)
}

func (e *TestEnv) NewRegistryProvider(workDir, certsDir string) RegistryProvider {
	switch spec := e.Config.Registry.(type) {
	case *ZotProviderSpec:
		return NewZotProvider(spec, workDir, certsDir)
	default:
		panic(fmt.Sprintf("unsupported registry provider type: %T", spec))
	}
}

func (e *TestEnv) NewClusterProvider(workDir string) ClusterProvider {
	switch spec := e.Config.Cluster.(type) {
	case *KindProviderSpec:
		return NewKindProvider(spec, workDir)
	default:
		panic(fmt.Sprintf("unsupported cluster provider type: %T", spec))
	}
}

func (e *TestEnv) NewCLIProvider(workDir, certsDir string) CLIProvider {
	switch spec := e.Config.CLI.(type) {
	case *ImageCLIProviderSpec:
		return NewOCMCLIProvider(spec, workDir, certsDir)
	case *BinaryCLIProviderSpec:
		panic("binary CLI provider not yet fully implemented in E2E setup")
	default:
		panic(fmt.Sprintf("unsupported CLI provider type: %T", spec))
	}
}

func (e *TestEnv) NewControllerProvider(workDir string, cluster ClusterProvider) ControllerProvider {
	return NewOCMControllerProvider(workDir, cluster)
}

// TestMeta defines metadata for a test, including labels and description.
type TestMeta struct {
	ID          string // Optional unique ID
	Description string
	Labels      map[string]string
}

// Bind binds the test metadata to the current test execution.
// It logs the test description.
func (m *TestMeta) Bind(t *testing.T) {
	t.Logf("Test: %s", t.Name())
	if m.Description != "" {
		t.Logf("Description: %s", m.Description)
	}
}

// HasLabel checks if the metadata contains the specified label with the given value.
func (m *TestMeta) HasLabel(key, value string) bool {
	if m.Labels == nil {
		return false
	}
	v, ok := m.Labels[key]
	return ok && v == value
}

// RequireLabel skips the test if the metadata does not contain the specified label with the given value.
func (m *TestMeta) RequireLabel(t *testing.T, key, value string) {
	if !m.HasLabel(key, value) {
		t.Skipf("Skipping test %s: required label %s=%s not found", t.Name(), key, value)
	}
}

// Common Labels
const (
	LabelTestKind    = "test-kind"
	ValueConformance = "conformance"
	ValueE2E         = "e2e"
)

// NewConformanceMeta creates a TestMeta for a conformance test.
func NewConformanceMeta(id, description string) *TestMeta {
	return &TestMeta{
		ID:          id,
		Description: description,
		Labels: map[string]string{
			LabelTestKind: ValueConformance,
		},
	}
}
