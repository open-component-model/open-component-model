package e2e

import (
	"log"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
)

// projectDir is the repository's controller root, used to resolve the two
// scenario discovery roots.
func projectDir() string {
	if dir := os.Getenv("PROJECT_DIR"); dir != "" {
		return dir
	}
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal("could not get current working directory", err)
	}
	return cwd
}

func discoverAndLoad(root string) []*ScenarioConfig {
	dirs, err := walkScenarios(root)
	if err != nil {
		log.Fatalf("walkScenarios(%q): %v", root, err)
	}
	configs := make([]*ScenarioConfig, 0, len(dirs))
	vars := builtinVars()
	compsDir := componentsDir()
	for _, dir := range dirs {
		cfg, err := loadScenario(dir, root, compsDir, vars)
		if err != nil {
			log.Fatalf("loadScenario(%q): %v", dir, err)
		}
		configs = append(configs, cfg)
	}
	return configs
}

var _ = Describe("scenarios", func() {
	dir := projectDir()
	examplesRoot := filepath.Join(dir, "examples")
	scenariosRoot := filepath.Join(dir, "test/e2e/scenarios")

	Context("examples", func() {
		for _, cfg := range discoverAndLoad(examplesRoot) {
			It("should run "+cfg.Folder, func(ctx SpecContext) {
				runScenario(ctx, cfg)
			})
		}
	})

	Context("scenarios", func() {
		for _, cfg := range discoverAndLoad(scenariosRoot) {
			It("should run "+cfg.Folder, func(ctx SpecContext) {
				runScenario(ctx, cfg)
			})
		}
	})
})
