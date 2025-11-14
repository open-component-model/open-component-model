package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/cli/cmd"
)

func AddComponentForConstructor(ctx context.Context, constructorContent string, cfgPath string, registryURL string) (err error) {
	constructorPath := filepath.Join(os.TempDir(), "constructor.yaml")
	defer func(name string) {
		err = os.Remove(name)
	}(constructorPath)

	if err = os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm); err != nil { //nolint:gosec // test code
		return err
	}

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", fmt.Sprintf("http://%s", registryURL),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})

	return addCMD.ExecuteContext(ctx)
}
