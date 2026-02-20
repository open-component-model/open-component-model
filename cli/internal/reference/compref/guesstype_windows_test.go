package compref

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_isWindowsAbsPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Positive cases
		{name: "drive letter backslash", input: `C:\path`, want: true},
		{name: "drive letter forward slash", input: "D:/path", want: true},
		{name: "lowercase drive letter", input: `c:\path`, want: true},
		{name: "drive letter nested path", input: `E:\Users\test\repos\archive`, want: true},
		{name: "drive Z", input: `Z:\data`, want: true},
		{name: "drive letter minimal path", input: `C:\x`, want: true},

		// Negative cases
		{name: "unix absolute path", input: "/tmp/ctf", want: false},
		{name: "relative path", input: "./local/path", want: false},
		{name: "domain with port", input: "localhost:5000", want: false},
		{name: "URL with scheme", input: "https://registry.io", want: false},
		{name: "empty string", input: "", want: false},
		{name: "single char", input: "C", want: false},
		{name: "drive letter only", input: "C:", want: false},
		{name: "digit drive letter", input: `1:\path`, want: false},
		{name: "multi-char before colon", input: `CD:\path`, want: false},
		{name: "drive letter no separator", input: "C:path", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isWindowsAbsPath(tt.input), "input %q", tt.input)
		})
	}
}

func Test_normalizeOS(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "backslashes to forward slashes", input: `C:\TEMP\ctf`, expected: "C:/TEMP/ctf"},
		{name: "mixed separators", input: `C:\Users/test\repos`, expected: "C:/Users/test/repos"},
		{name: "forward slashes unchanged", input: "D:/TEMP/ctf", expected: "D:/TEMP/ctf"},
		{name: "unix path unchanged", input: "/tmp/archive", expected: "/tmp/archive"},
		{name: "relative path unchanged", input: "./local/path", expected: "./local/path"},
		{name: "empty string", input: "", expected: ""},
		{name: "deeply nested backslash path", input: `C:\a\b\c\d\e`, expected: "C:/a/b/c/d/e"},
		{name: "trailing backslash", input: `D:\data\`, expected: "D:/data/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeOS(tt.input))
		})
	}
}

func Test_guessTypeOS(t *testing.T) {
	ctfType := runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String()

	tests := []struct {
		name     string
		input    string
		wantType string
		wantOK   bool
	}{
		{name: "backslash path", input: `C:\TEMP\ctf`, wantType: ctfType, wantOK: true},
		{name: "forward slash path", input: "D:/TEMP/ctf", wantType: ctfType, wantOK: true},
		{name: "lowercase drive", input: `c:\data`, wantType: ctfType, wantOK: true},
		{name: "unix absolute path", input: "/tmp/ctf", wantType: "", wantOK: false},
		{name: "relative path", input: "./local/path", wantType: "", wantOK: false},
		{name: "domain with port", input: "localhost:5000", wantType: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotOK := guessTypeOS(tt.input)
			assert.Equal(t, tt.wantOK, gotOK, "unexpected ok for input %q", tt.input)
			assert.Equal(t, tt.wantType, gotType, "unexpected type for input %q", tt.input)
		})
	}
}

func Test_guessType_windows(t *testing.T) {
	ctfType := runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String()
	ociType := runtime.NewVersionedType(ociv1.Type, ociv1.Version).String()

	tests := []struct {
		name         string
		input        string
		expectedType string
	}{
		// Windows paths should be detected as CTF before URL parsing mistakes them for schemes
		{name: "backslash path", input: `C:\TEMP\ctf`, expectedType: ctfType},
		{name: "forward slash path", input: "D:/TEMP/ctf", expectedType: ctfType},
		{name: "lowercase drive", input: `c:\data`, expectedType: ctfType},

		// Non-Windows inputs should still work through the regular heuristics
		{name: "domain", input: "ghcr.io/my-org/repo", expectedType: ociType},
		{name: "localhost with port", input: "localhost:5000/repo", expectedType: ociType},
		{name: "relative path", input: "./local/archive", expectedType: ctfType},
		{name: "absolute unix path", input: "/tmp/ctf", expectedType: ctfType},
		{name: "file URL", input: "file://./archive", expectedType: ctfType},
		{name: "https URL", input: "https://registry.io/repo", expectedType: ociType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, err := guessType(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedType, gotType, "unexpected type for input %q", tt.input)
		})
	}
}
