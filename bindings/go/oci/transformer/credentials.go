package transformer

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
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
	case *ocicredsv1.OCICredentials:
		result := map[string]string{}
		if c.Username != "" {
			result[ocicredsv1.CredentialKeyUsername] = c.Username
		}
		if c.Password != "" {
			result[ocicredsv1.CredentialKeyPassword] = c.Password
		}
		if c.AccessToken != "" {
			result[ocicredsv1.CredentialKeyAccessToken] = c.AccessToken
		}
		if c.RefreshToken != "" {
			result[ocicredsv1.CredentialKeyRefreshToken] = c.RefreshToken
		}
		return result, nil
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
