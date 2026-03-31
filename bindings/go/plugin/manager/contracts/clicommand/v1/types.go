package v1

import "ocm.software/open-component-model/bindings/go/runtime"

// GetCredentialConsumerIdentityRequest is sent to the plugin to ask which
// credential consumer identity is needed for this invocation.
type GetCredentialConsumerIdentityRequest struct {
	// Verb and ObjectType identify which command is being invoked.
	Verb       string `json:"verb"`
	ObjectType string `json:"objectType"`
	// Flags are the resolved flag values for this invocation.
	// The plugin may use them (e.g. a target URL) to construct the right identity.
	Flags map[string]string `json:"flags,omitempty"`
}

// GetCredentialConsumerIdentityResponse carries the credential consumer identity
// back to the CLI. An empty Identity means no credentials are needed.
type GetCredentialConsumerIdentityResponse struct {
	// Identity is the consumer identity to resolve credentials for.
	Identity runtime.Identity `json:"identity,omitempty"`
}

// ExecuteRequest carries the full invocation context to the plugin.
type ExecuteRequest struct {
	Verb       string            `json:"verb"`
	ObjectType string            `json:"objectType"`
	Args       []string          `json:"args,omitempty"`
	Flags      map[string]string `json:"flags,omitempty"`
}

// ExecuteResponse carries the result back to the CLI.
type ExecuteResponse struct {
	// ExitCode != 0 causes the CLI to exit with that code after printing Output.
	ExitCode int `json:"exitCode,omitempty"`
	// Output is written to stdout. For long-running commands, plugins should
	// prefer writing structured logs to stderr (forwarded by StartLogStreamer)
	// rather than buffering large output here.
	Output string `json:"output,omitempty"`
}
