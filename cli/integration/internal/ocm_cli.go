package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func AddComponentForConstructor(ctx context.Context, addCMD *cobra.Command, constructorContent string, cfgPath string, registryURL string) (err error) {
	constructorPath := filepath.Join(os.TempDir(), "constructor.yaml")
	defer os.Remove(constructorPath)

	if err = os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm); err != nil { //nolint:gosec // test code
		return err
	}

	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", fmt.Sprintf("http://%s", registryURL),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})

	return addCMD.ExecuteContext(ctx)
}
