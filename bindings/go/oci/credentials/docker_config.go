package credentials

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"

	"oras.land/oras-go/v2/registry/remote/auth"
	remotecredentials "oras.land/oras-go/v2/registry/remote/credentials"

	credentialsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Credential key constants re-exported from the typed credential package for backward compatibility.
const (
	// CredentialKeyUsername is the key for basic auth username.
	CredentialKeyUsername = credentialsv1.CredentialKeyUsername
	// CredentialKeyPassword is the key for basic auth password.
	CredentialKeyPassword = credentialsv1.CredentialKeyPassword
	// CredentialKeyAccessToken is the key for OAuth2/bearer access tokens.
	CredentialKeyAccessToken = credentialsv1.CredentialKeyAccessToken
	// CredentialKeyRefreshToken is the key for OAuth2 refresh tokens.
	CredentialKeyRefreshToken = credentialsv1.CredentialKeyRefreshToken
)

// CredentialFromMap converts a credential map to an auth.Credential.
//
// Deprecated: Use CredentialFromTyped instead.
func CredentialFromMap(credentials map[string]string) auth.Credential {
	cred := auth.Credential{}
	if v, ok := credentials[CredentialKeyUsername]; ok {
		cred.Username = v
	}
	if v, ok := credentials[CredentialKeyPassword]; ok {
		cred.Password = v
	}
	if v, ok := credentials[CredentialKeyAccessToken]; ok {
		cred.AccessToken = v
	}
	if v, ok := credentials[CredentialKeyRefreshToken]; ok {
		cred.RefreshToken = v
	}
	return cred
}

// CredentialFunc creates a function that returns credentials based on host and port matching.
// It takes an identity map and a credentials map as input and returns a function that can be
// used with the ORAS client for authentication.
//
// The returned function will:
//   - Return the provided credentials if the host and port match the identity
//   - Return empty credentials if there's a mismatch
//   - Return an error if the hostport string is invalid
//
// Example:
//
//	identity := runtime.Identity{
//		runtime.IdentityAttributeHostname: "example.com",
//		runtime.IdentityAttributePort:     "443",
//	}
//	credentials := map[string]string{
//		CredentialKeyUsername: "user",
//		CredentialKeyPassword: "pass",
//	}
//	credFunc := CredentialFunc(identity, credentials)
//
// This will create a function that checks if the host and port match "example.com:443",
// and returns the provided credentials if they do. If the host and port don't match,
// it will return empty credentials.
//
// Deprecated: Use CredentialFuncTyped instead.
func CredentialFunc(identity runtime.Identity, credentials map[string]string) auth.CredentialFunc {
	credential := CredentialFromMap(credentials)
	registeredHostname, hostInIdentity := identity[runtime.IdentityAttributeHostname]
	registeredPort, portInIdentity := identity[runtime.IdentityAttributePort]

	return func(ctx context.Context, hostport string) (auth.Credential, error) {
		actualHost, actualPort, err := net.SplitHostPort(hostport)
		if err != nil {
			// it can happen that no port is given here
			err, addrError := errors.AsType[*net.AddrError](err)
			portIsMissing := addrError && err.Err == "missing port in address"
			if !portIsMissing {
				return auth.Credential{}, fmt.Errorf("failed to split host and port: %w", err)
			}
			actualHost = hostport
		}
		hostMismatch := hostInIdentity && registeredHostname != actualHost
		portMismatch := portInIdentity && registeredPort != actualPort
		if hostMismatch || portMismatch {
			return auth.EmptyCredential, nil
		}
		return credential, nil
	}
}

// CredentialFromTyped converts a [credentialsv1.OCICredentials] to an auth.Credential.
// A nil input yields an empty [auth.Credential].
func CredentialFromTyped(credentials *credentialsv1.OCICredentials) auth.Credential {
	if credentials == nil {
		return auth.Credential{}
	}
	cred := auth.Credential{}
	cred.Username = credentials.Username
	cred.Password = credentials.Password
	cred.AccessToken = credentials.AccessToken
	cred.RefreshToken = credentials.RefreshToken
	return cred
}

// CredentialFuncTyped creates a function that returns credentials based on host and port matching.
// It takes an identity map and [credentialsv1.OCICredentials] as input and returns a function that can be
// used with the ORAS client for authentication.
//
// The returned function will:
//   - Return the provided credentials if the host and port match the identity
//   - Return empty credentials if there's a mismatch
//   - Return an error if the hostport string is invalid
//
// Example:
//
//	identity := &identityv1.OCIRegistryIdentity{
//		Hostname: "example.com",
//		Port:     "443",
//	}
//	credentials := &credentialsv1.OCICredentials{
//		Username: "user",
//		Password: "pass",
//	}
//	credFunc := CredentialFuncTyped(identity, credentials)
//
// This will create a function that checks if the host and port match "example.com:443",
// and returns the provided credentials if they do. If the host and port don't match,
// it will return empty credentials.
func CredentialFuncTyped(identity *identityv1.OCIRegistryIdentity, credentials *credentialsv1.OCICredentials) auth.CredentialFunc {
	credential := CredentialFromTyped(credentials)
	hasHost := identity != nil && identity.Hostname != ""
	hasPort := identity != nil && identity.Port != ""

	return func(ctx context.Context, hostport string) (auth.Credential, error) {
		actualHost, actualPort, err := net.SplitHostPort(hostport)
		if err != nil {
			// it can happen that no port is given here
			err, addrError := errors.AsType[*net.AddrError](err)
			portIsMissing := addrError && err.Err == "missing port in address"
			if !portIsMissing {
				return auth.Credential{}, fmt.Errorf("failed to split host and port: %w", err)
			}
			actualHost = hostport
		}
		hostMismatch := hasHost && identity.Hostname != actualHost
		portMismatch := hasPort && identity.Port != actualPort
		if hostMismatch || portMismatch {
			return auth.EmptyCredential, nil
		}
		return credential, nil
	}
}

// Resolving of credentials is an expensive operation and we might receive many requests from different
// graph transformers or nodes requesting access to a registry at once. In this case it is advisable to
// deduplicate the requests as most credential helpers do not deal well with multiple concurrent calls due
// to the protocol relying on binary execution on many operating systems.
var storeConcurrencyMu sync.Mutex

// ResolveV1DockerConfigCredentials resolves credentials from a Docker configuration
// for a given identity. It supports both file-based and in-memory Docker configurations.
//
// The function will:
//   - Load credentials from the specified Docker config source
//   - Match the credentials against the provided identity
//   - Return a map of credential key-value pairs
func ResolveV1DockerConfigCredentials(ctx context.Context, dockerConfig credentialsv1.DockerConfig, identity runtime.Identity) (map[string]string, error) {
	credStore, err := getStore(ctx, dockerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve credentials store: %w", err)
	}

	oci := identityv1.FromIdentity(identity)
	if oci == nil || oci.Hostname == "" {
		slog.DebugContext(ctx, "no hostname provided, skipping credential resolution, since docker configs"+
			"cannot resolve without hostname", "identity", identity)
		return nil, nil
	}

	hostname := oci.Hostname
	// this is a special case
	// The Docker CLI expects that the credentials of
	// the registry 'docker.io' will be added under the key "https://index.docker.io/v1/".
	// See: https://github.com/moby/moby/blob/v24.0.2/registry/config.go#L25-L48
	if hostname == "docker.io" {
		hostname = "index.docker.io"
	}

	logger := slog.With("hostname", hostname, "identity", identity.String())
	storeConcurrencyMu.Lock()
	defer storeConcurrencyMu.Unlock()
	logger.DebugContext(ctx, "getting credentials")
	cred, err := credStore.Get(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials for %q: %w", hostname, err)
	}
	if cred == auth.EmptyCredential && oci.Port != "" {
		// because ORAS stores docker credentials including its port if defined, we'll try
		// once more including the port with the hostname. (For ghcr.io no port is there, but we
		// default the port to 443 so trying it with that in the first time would fail that's why
		// this is a fallback try).
		hostname = fmt.Sprintf("%s:%s", hostname, oci.Port)
		logger.DebugContext(ctx, "attempting secondary credentials lookup via hostname:port")
		cred, err = credStore.Get(ctx, hostname)
		if err != nil {
			return nil, fmt.Errorf("failed to get credentials for %q with port: %w", hostname, err)
		}
	}
	if cred == auth.EmptyCredential {
		logger.DebugContext(ctx, "no credentials found")
		return nil, nil
	}

	credentialMap := map[string]string{}
	if v := cred.Username; v != "" {
		credentialMap[CredentialKeyUsername] = v
	}
	if v := cred.Password; v != "" {
		credentialMap[CredentialKeyPassword] = v
	}
	if v := cred.AccessToken; v != "" {
		credentialMap[CredentialKeyAccessToken] = v
	}
	if v := cred.RefreshToken; v != "" {
		credentialMap[CredentialKeyRefreshToken] = v
	}

	logger.DebugContext(ctx, "credentials found", "username", cred.Username)

	return credentialMap, nil
}

// getStore creates a credential store based on the provided Docker configuration.
// It supports three modes of operation:
//   - Default mode: Uses system default Docker config locations
//   - Inline config: Uses a provided JSON configuration string
//   - File-based: Uses a specified Docker config file
//
// The function handles:
//   - Shell expansion for file paths (e.g., ~ for home directory)
//   - Temporary file creation for inline configurations
//   - Integration with the native host credential store
func getStore(ctx context.Context, dockerConfig credentialsv1.DockerConfig) (remotecredentials.Store, error) {
	// Determine which store creation strategy to use based on the provided configuration
	switch {
	case dockerConfig.DockerConfigFile == "" && dockerConfig.DockerConfig == "":
		return createDefaultStore(ctx)
	case dockerConfig.DockerConfig != "":
		return createInlineConfigStore(ctx, dockerConfig.DockerConfig)
	case dockerConfig.DockerConfigFile != "":
		return createFileBasedStore(ctx, dockerConfig.DockerConfigFile)
	default:
		return nil, fmt.Errorf("invalid docker config: neither default, inline config, nor config file specified")
	}
}

// createDefaultStore creates a credential store using system default Docker config locations
// and attempts to use the native host credential store if available.
func createDefaultStore(ctx context.Context) (remotecredentials.Store, error) {
	slog.DebugContext(ctx, "attempting to load docker config from default locations or native host store")
	store, err := remotecredentials.NewStoreFromDocker(remotecredentials.StoreOptions{
		DetectDefaultNativeStore: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create default docker config store: %w", err)
	}
	return store, nil
}

// createInlineConfigStore creates a credential store from an inline JSON configuration.
// It creates a temporary file to store the configuration and loads it into memory.
func createInlineConfigStore(ctx context.Context, config string) (remotecredentials.Store, error) {
	slog.DebugContext(ctx, "using docker config from inline config")
	// Create and return the store
	store, err := remotecredentials.NewMemoryStoreFromDockerConfig([]byte(config))
	if err != nil {
		return nil, fmt.Errorf("failed to create inline config store: %w", err)
	}
	return store, nil
}

// createFileBasedStore creates a credential store from a specified Docker config file.
// It handles shell expansion for the file path and validates the file's existence.
func createFileBasedStore(ctx context.Context, configPath string) (remotecredentials.Store, error) {
	slog.DebugContext(ctx, "using docker config from file", "file", configPath)

	// Handle shell expansion for the config path
	expandedPath, err := expandConfigPath(configPath)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(expandedPath); err != nil {
		slog.WarnContext(ctx, "failed to find docker config file, thus the config will not offer any credentials", "path", expandedPath)
	}

	// Create and return the store
	// For file-based stores, if the file does not exist,
	// it counts as an empty store and will not fail here!
	store, err := remotecredentials.NewStore(expandedPath, remotecredentials.StoreOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create file-based config store: %w", err)
	}
	return store, nil
}

// expandConfigPath handles shell expansion for the config file path.
// Currently supports basic home directory expansion (~).
func expandConfigPath(path string) (string, error) {
	if idx := strings.Index(path, "~"); idx != -1 {
		dirname, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		return strings.ReplaceAll(path, "~", dirname), nil
	}
	return path, nil
}
