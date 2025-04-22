package types

import "os/exec"

// Plugin represents a connected plugin
type Plugin struct {
	ID     string
	Path   string
	Config Config
	Types  map[PluginType][]Type

	Cmd *exec.Cmd
}
