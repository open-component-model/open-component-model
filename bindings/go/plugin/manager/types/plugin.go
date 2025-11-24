package types

import (
	"io"
	"os/exec"
)

// Plugin has information about the given plugin backed by the constructed CMD. This command will be called
// during the fetch operation to actually start plugin.
type Plugin struct {
	ID     string
	Path   string
	Config Config
	Types  map[PluginType][]Type
	// []Type{inputTypeFunctionA, outputTypeFunctionA, inputTypeFunctionB, outputTypeFunctionB} <-- we implicitly know because of the plugin type that [0] is input of method a and [1] is output of method a, and so on.
	//            ^ is this only the typed part (so repository spec for upload component) or the entire request type (so repository spec + component descriptor for upload component)?

	Cmd *exec.Cmd
	// Stderr pipe will contain a link to the commands stderr output to stream back
	// potential more information to the manager or the runtime.
	Stderr io.ReadCloser
	// Stdout pipe is a link to the plugin's output. This is the standard output to fetch
	// location data from the plugin once the plugin is started.
	Stdout io.ReadCloser
}
