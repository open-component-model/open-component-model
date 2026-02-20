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
	// TimeoutFlag Flag to specify the HTTP client timeout, overriding the config file value.
	TimeoutFlag = "timeout"
	// TCPDialTimeoutFlag Flag to specify the TCP dial timeout (TCP connection establishment).
	TCPDialTimeoutFlag = "tcp-dial-timeout"
	// TCPKeepAliveFlag Flag to specify the TCP keep-alive interval.
	TCPKeepAliveFlag = "tcp-keep-alive"
	// TLSHandshakeTimeoutFlag Flag to specify the TLS handshake timeout.
	TLSHandshakeTimeoutFlag = "tls-handshake-timeout"
	// ResponseHeaderTimeoutFlag Flag to specify the response header timeout.
	ResponseHeaderTimeoutFlag = "response-header-timeout"
	// IdleConnTimeoutFlag Flag to specify the idle connection timeout.
	IdleConnTimeoutFlag = "idle-conn-timeout"
)
