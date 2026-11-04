package v1

const (
	Version = "v1"
	//nolint:gosec // G101: This is a type name, not a credential.
	GitHTTPSCredentialsType = "GitHTTPSCredentials"
	//nolint:gosec // G101: This is a type name, not a credential.
	GitBearerCredentialsType = "GitBearerCredentials"
	//nolint:gosec // G101: This is a type name, not a credential.
	GitSSHCredentialsType = "GitSSHCredentials"
	GitCredentialsType    = "GitCredentials"
)
