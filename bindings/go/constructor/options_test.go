package constructor

import (
	"os"
	"testing"
)

func TestOptions_SpecFileDir_Unix(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("This test is only for Unix-like systems")
	}
	
	opts := &Options{
		SpecFilePath: "/path/to/spec/constructor.yaml",
	}

	expectedDir := "/path/to/spec"

	if dir := opts.SpecFileDir(); dir != expectedDir {
		t.Errorf("Expected SpecFileDir to be %s, got %s", expectedDir, dir)
	}
}

func TestOptions_SpecFileDir_Windows(t *testing.T) {
	if os.PathSeparator != '\\' {
		t.Skip("This test is only for Windows")
	}

	opts := &Options{
		SpecFilePath: "C:\\path\\to\\spec\\constructor.yaml",
	}

	expectedDir := "C:\\path\\to\\spec"

	if dir := opts.SpecFileDir(); dir != expectedDir {
		t.Errorf("Expected SpecFileDir to be %s, got %s", expectedDir, dir)
	}
}
