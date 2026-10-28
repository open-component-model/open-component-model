package plugins

import (
	"net/http"
	"os/exec"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// Process is a started plugin process together with the connection used to talk to it.
type Process struct {
	Cmd      *exec.Cmd
	Client   *http.Client
	Type     types.ConnectionType
	Location string
}

// NewProcess returns the Process of a plugin that was started and waited for with WaitForPlugin.
func NewProcess(plugin *types.Plugin, client *http.Client, location string) Process {
	return Process{
		Cmd:      plugin.Cmd,
		Client:   client,
		Type:     plugin.Config.Type,
		Location: location,
	}
}
