package v1

import (
	"context"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CLICommandPluginContract is the interface a plugin binary must implement
// for each CLI command it registers.
type CLICommandPluginContract interface {
	contracts.PluginBase
	// GetCLICommandCredentialConsumerIdentity returns the credential consumer
	// identity for this command invocation. The CLI resolves credentials from
	// the credential graph and passes them back via Execute.
	// Return an empty or nil identity to indicate no credentials are needed.
	GetCLICommandCredentialConsumerIdentity(
		ctx context.Context,
		req *GetCredentialConsumerIdentityRequest,
	) (*GetCredentialConsumerIdentityResponse, error)
	// Execute runs the command with the provided args, flags, and resolved credentials.
	Execute(
		ctx context.Context,
		req *ExecuteRequest,
		credentials map[string]string,
	) (*ExecuteResponse, error)
}

// BuiltinCLICommandPlugin is for internal (in-process) CLI command plugin implementations.
// External plugins communicate via HTTP and don't need to implement this interface.
type BuiltinCLICommandPlugin interface {
	CLICommandPluginContract
	// GetCommandSpec returns the static command metadata.
	GetCommandSpec() CommandSpec
	// GetCLICommandScheme returns the scheme used by this plugin.
	// Required for internal registration without an external scheme.
	GetCLICommandScheme() *runtime.Scheme
}
