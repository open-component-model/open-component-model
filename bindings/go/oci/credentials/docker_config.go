package credentials

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	"oras.land/oras-go/v2/registry/remote/auth"
	remotecredentials "oras.land/oras-go/v2/registry/remote/credentials"

	credentialsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CredentialKey constants define the standard keys used in credential maps.
// These keys are used to store and retrieve different types of credentials.
const (
	// CredentialKeyUsername is the key for storing username credentials
	CredentialKeyUsername = "username"
	// CredentialKeyPassword is the key for storing password credentials
	CredentialKeyPassword = "password"
	// CredentialKeyAccessToken is the key for storing access token credentials
	CredentialKeyAccessToken = "accessToken"
	// CredentialKeyRefreshToken is the key for storing refresh token credentials
	CredentialKeyRefreshToken = "refreshToken"
)

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
func CredentialFunc(identity runtime.Identity, credentials map[string]string) auth.CredentialFunc {
	credential := auth.Credential{}
	if v, ok := credentials[CredentialKeyUsername]; ok {
		credential.Username = v
	}
	if v, ok := credentials[CredentialKeyPassword]; ok {
		credential.Password = v
	}
	if v, ok := credentials[CredentialKeyAccessToken]; ok {
		credential.AccessToken = v
	}
	if v, ok := credentials[CredentialKeyRefreshToken]; ok {
		credential.RefreshToken = v
	}
	registeredHostname, hostInIdentity := identity[runtime.IdentityAttributeHostname]
	registeredPort, portInIdentity := identity[runtime.IdentityAttributePort]

	return func(ctx context.Context, hostport string) (auth.Credential, error) {
		actualHost, actualPort, err := net.SplitHostPort(hostport)
		if err != nil {
			return auth.Credential{}, fmt.Errorf("failed to split host and port: %w", err)
		}
		hostMismatch := hostInIdentity && registeredHostname != actualHost
		portMismatch := portInIdentity && registeredPort != actualPort
		if hostMismatch || portMismatch {
			return auth.EmptyCredential, nil
		}
		return credential, nil
	}
}

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

	hostname := identity[runtime.IdentityAttributeHostname]
	if hostname == "" {
		return nil, fmt.Errorf("missing %q in identity", runtime.IdentityAttributeHostname)
	}

	cred, err := credStore.Get(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials for %q: %w", hostname, err)
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
	var store remotecredentials.Store
	var err error
	switch {
	case dockerConfig.DockerConfigFile == "" && dockerConfig.DockerConfig == "":
		slog.InfoContext(ctx, "attempting to load docker config from default locations or native host store")
		store, err = remotecredentials.NewStoreFromDocker(remotecredentials.StoreOptions{
			DetectDefaultNativeStore: true,
		})
	case dockerConfig.DockerConfig != "":
		slog.InfoContext(ctx, "using docker config from inline config")
		var tmp *os.File
		if tmp, err = os.CreateTemp("", "docker-config-*.json"); err != nil {
			return nil, fmt.Errorf("failed to create temporary file: %w", err)
		}
		defer func() {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name()) // we can safely remove the temp file because the store is loaded into memory
		}()
		if err := os.WriteFile(tmp.Name(), []byte(dockerConfig.DockerConfig), 0o600); err != nil {
			return nil, fmt.Errorf("failed to write temporary file: %w", err)
		}
		store, err = remotecredentials.NewStore(tmp.Name(), remotecredentials.StoreOptions{})
	case dockerConfig.DockerConfigFile != "":
		slog.InfoContext(ctx, "using docker config from file", "file", dockerConfig.DockerConfigFile)
		path := dockerConfig.DockerConfigFile
		// old OCM versions did unclean shell expansion...
		// simple shell expansion for legacy compatibility, does not support cross user expansion
		if idx := strings.Index(path, "~"); idx != -1 {
			dirname, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to get user home directory: %w", err)
			}
			path = strings.ReplaceAll(path, "~", dirname)
		}
		if _, err := os.Stat(path); err != nil {
			slog.WarnContext(ctx, "failed to find docker config file, thus the config will not offer any credentials", "path", path)
		}
		store, err = remotecredentials.NewStore(path, remotecredentials.StoreOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create docker config store: %w", err)
	}
	return wrapWithLogging(store, slog.Default()), nil
}
