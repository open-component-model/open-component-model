package componentversionrepository

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type envTest struct {
	pluginLocation string
}

var testEnv envTest

func TestMain(m *testing.M) {
	tmp := os.TempDir()
	binary := filepath.Join(tmp, "test-plugin")
	// Compile the plugin using `go build`
	cmd := exec.Command("go", "build", "-o", binary, filepath.Join("testdata", "generic_plugin", "main.go"))
	content, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatal(fmt.Errorf("error building plugin binary: %s; content: %s", err, string(content)))
	}
	defer os.RemoveAll(tmp)

	testEnv = envTest{
		pluginLocation: binary,
	}

	exitCode := m.Run()
	os.Exit(exitCode)
}
