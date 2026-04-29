package transformation

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// resolveCredentialsMap calls ResolveTyped and converts the result to map[string]string
// for downstream interfaces that haven't migrated to runtime.Typed yet (Phase 4).
// Returns nil, nil if no credentials are found.
func resolveCredentialsMap(ctx context.Context, resolver credentials.Resolver, identity runtime.Typed) (map[string]string, error) {
	typed, err := resolver.ResolveTyped(ctx, identity)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	switch c := typed.(type) {
	case *helmcredsv1.HelmHTTPCredentials:
		return map[string]string{
			helmcredsv1.CredentialKeyUsername: c.Username,
			helmcredsv1.CredentialKeyPassword: c.Password,
			helmcredsv1.CredentialKeyCertFile: c.CertFile,
			helmcredsv1.CredentialKeyKeyFile:  c.KeyFile,
			helmcredsv1.CredentialKeyKeyring:  c.Keyring,
		}, nil
	case *credconfigv1.DirectCredentials:
		result := make(map[string]string, len(c.Properties))
		for k, v := range c.Properties {
			result[k] = v
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported credential type %T", typed)
	}
}
