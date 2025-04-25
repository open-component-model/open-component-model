package types

import "os/exec"

// Plugin has information about the given plugin backed by the constructed CMD. This command will be called
// during the fetch operation to actually start plugin.
type Plugin struct {
	ID     string
	Path   string
	Config Config
	Types  map[PluginType][]Type

	Cmd *exec.Cmd
}
