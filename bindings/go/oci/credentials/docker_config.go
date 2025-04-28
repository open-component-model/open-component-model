package credentials

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"oras.land/oras-go/v2/registry/remote/auth"
	remotecredentials "oras.land/oras-go/v2/registry/remote/credentials"

	ocicredentials "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	credentialsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type DockerConfigResolver struct{}

func (p DockerConfigResolver) Resolve(ctx context.Context, cfg runtime.Typed, identity runtime.Identity, _ map[string]string) (map[string]string, error) {
	dockerConfig := credentialsv1.DockerConfig{}
	if err := ocicredentials.Scheme.Convert(cfg, &dockerConfig); err != nil {
		return nil, fmt.Errorf("failed to resolve credentials because config could not be interpreted as docker config: %v", err)
	}

	credStore, err := getStore(ctx, dockerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve credentials store: %v", err)
	}

	hostname := identity[runtime.IdentityAttributeHostname]
	if hostname == "" {
		return nil, fmt.Errorf("missing %q in identity", runtime.IdentityAttributeHostname)
	}

	cred, err := credStore.Get(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials for %q: %v", hostname, err)
	}
	credentialMap := map[string]string{}
	if v := cred.Username; v != "" {
		credentialMap["username"] = v
	}
	if v := cred.Password; v != "" {
		credentialMap["password"] = v
	}
	if v := cred.AccessToken; v != "" {
		credentialMap["accessToken"] = v
	}
	if v := cred.RefreshToken; v != "" {
		credentialMap["refreshToken"] = v
	}

	return credentialMap, nil
}

func (p DockerConfigResolver) SupportedRepositoryConfigTypes() []runtime.Type {
	return []runtime.Type{
		ocicredentials.CredentialRepositoryConfigType,
	}
}

func (p DockerConfigResolver) ConsumerIdentityForConfig(_ context.Context, _ runtime.Typed) (runtime.Identity, error) {
	return nil, fmt.Errorf("consumer identities are not necessary for a docker config file and is thus not supported")
}

func getStore(ctx context.Context, dockerConfig credentialsv1.DockerConfig) (remotecredentials.Store, error) {
	detect := dockerConfig.DockerConfigFile == "" && dockerConfig.DockerConfig == ""
	var store remotecredentials.Store
	var err error
	if detect {
		store, err = remotecredentials.NewStoreFromDocker(remotecredentials.StoreOptions{
			DetectDefaultNativeStore: true,
		})
	} else if dockerConfig.DockerConfig != "" {
		slog.InfoContext(ctx, "using docker config from inline config")
		// TODO(jakobmoellerdev) change from temp file to inmemory store, missing oras implementation for this
		tmp, err := os.CreateTemp("", "docker-config-*.json")
		if err != nil {
			return nil, fmt.Errorf("failed to create temporary file: %w", err)
		}
		if err := os.WriteFile(tmp.Name(), []byte(dockerConfig.DockerConfig), 0644); err != nil {
			return nil, fmt.Errorf("failed to write temporary file: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return nil, fmt.Errorf("failed to close temporary file: %w", err)
		}
		store, err = remotecredentials.NewStore(tmp.Name(), remotecredentials.StoreOptions{})
	} else if dockerConfig.DockerConfigFile != "" {
		path, err := resolveDockerConfigFilePathWithShellExpansion(dockerConfig.DockerConfigFile)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve docker config file %q: %w", path, err)
		}
		slog.InfoContext(ctx, "using docker config from file", "file", path)
		store, err = remotecredentials.NewStore(path, remotecredentials.StoreOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create docker config store: %w", err)
	}
	store = WrapWithLogging(store, slog.Default())
	return store, nil
}

// old OCM versions did unclean shell expansion...
func resolveDockerConfigFilePathWithShellExpansion(path string) (string, error) {
	// simple shell expansion for legacy compatibility, does not support cross user expansion
	if strings.HasPrefix(path, "~/") {
		dirname, _ := os.UserHomeDir()
		path = filepath.Join(dirname, path[2:])
	}
	return path, nil
}

func WrapWithLogging(store remotecredentials.Store, base *slog.Logger) remotecredentials.Store {
	return &loggingStore{store, base}
}

type loggingStore struct {
	remotecredentials.Store
	base *slog.Logger
}

func (l *loggingStore) Get(ctx context.Context, serverAddress string) (auth.Credential, error) {
	logger := l.base.With("serverAddress", serverAddress)
	logger.DebugContext(ctx, "getting credentials")
	credential, err := l.Store.Get(ctx, serverAddress)
	if err != nil {
		logger.ErrorContext(ctx, "failed to get credential", "error", err)
	} else if credential != auth.EmptyCredential {
		logger.InfoContext(ctx, "got credential", "username", credential.Username, "serverAddress", serverAddress)
	} else {
		logger.InfoContext(ctx, "got no credential")
	}
	return credential, err
}
