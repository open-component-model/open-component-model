package global

import "time"

const (
	TempFolderFlag               = "temp-folder"
	WorkingDirectoryFlag         = "working-directory"
	PluginShutdownTimeoutFlag    = "plugin-shutdown-timeout"
	PluginShutdownTimeoutDefault = 10 * time.Second
	PluginDirectoryFlag          = "plugin-directory"
)
