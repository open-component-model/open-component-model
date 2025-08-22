package cmd

import (
	"time"
)

const (
	// TempFolderFlag Flag to specify a custom temporary folder path for filesystem operations.
	TempFolderFlag = "temp-folder"
	// WorkingDirectoryFlag Flag to specify a custom working directory path to load resources from. All referenced resources must be located in this directory or its sub-directories.
	WorkingDirectoryFlag = "working-directory"
	// PluginShutdownTimeoutFlag Flag to specify the timeout for plugin shutdown. If a plugin does not shut down within this time, it is forcefully killed.
	PluginShutdownTimeoutFlag = "plugin-shutdown-timeout"
	// PluginShutdownTimeoutDefault Default timeout for plugin shutdown.
	PluginShutdownTimeoutDefault = 10 * time.Second
	// PluginDirectoryFlag Flag to specify the default directory path for OCM plugins.
	PluginDirectoryFlag = "plugin-directory"
)
