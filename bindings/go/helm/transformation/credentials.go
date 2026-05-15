package transformation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"

	"ocm.software/open-component-model/bindings/go/credentials"
	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// resolveCredentialsMap calls ResolveTyped and converts the result to map[string]string
// for downstream interfaces that haven't migrated to runtime.Typed yet (Phase 4).
// Returns nil, nil if no credentials are found.
func resolveCredentialsMap(ctx context.Context, resolver credentials.Resolver, identity runtime.Identity) (map[string]string, error) {
	typed, err := resolver.ResolveTyped(ctx, identity)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			slog.WarnContext(ctx, "no credentials found for identity", "identity", identity)
			return nil, nil
		}
		return nil, err
	}

	if typed == nil {
		return nil, nil
	}

	result := map[string]string{}
	switch c := typed.(type) {
	case *helmcredsv1.HelmHTTPCredentials:
		if c.Username != "" {
			result[helmcredsv1.CredentialKeyUsername] = c.Username
		}
		if c.Password != "" {
			result[helmcredsv1.CredentialKeyPassword] = c.Password
		}
		if c.CertFile != "" {
			result[helmcredsv1.CredentialKeyCertFile] = c.CertFile
		}
		if c.KeyFile != "" {
			result[helmcredsv1.CredentialKeyKeyFile] = c.KeyFile
		}
		if c.Keyring != "" {
			result[helmcredsv1.CredentialKeyKeyring] = c.Keyring
		}
	case *credconfigv1.DirectCredentials:
		result = maps.Clone(c.Properties)
	default:
		return nil, fmt.Errorf("unsupported credential type %T", typed)
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}
