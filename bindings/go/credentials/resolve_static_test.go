package credentials_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestStaticCredentialsResolver(t *testing.T) {
	credMap := map[string]map[string]string{
		"hostname=docker.io,type=OCIRegistry": {
			"username": "testuser",
			"password": "testpass",
		},
		"hostname=quay.io,type=OCIRegistry": {
			"username": "quayuser",
			"password": "quaypass",
		},
	}

	resolver := credentials.NewStaticCredentialsResolver(credMap)
	r := require.New(t)

	resolveMap := func(t *testing.T, identity runtime.Identity) (map[string]string, error) {
		t.Helper()
		typed, err := resolver.Resolve(context.Background(), identity)
		if err != nil {
			return nil, err
		}
		return typed.(*v1.DirectCredentials).Properties, nil
	}

	t.Run("resolve existing credentials", func(t *testing.T) {
		identity := runtime.Identity{
			"type":     "OCIRegistry",
			"hostname": "docker.io",
		}
		creds, err := resolveMap(t, identity)
		r.NoError(err)
		r.Equal("testuser", creds["username"])
		r.Equal("testpass", creds["password"])
	})

	t.Run("resolve not found", func(t *testing.T) {
		identity := runtime.Identity{
			"type":     "OCIRegistry",
			"hostname": "notfound.io",
		}
		creds, err := resolveMap(t, identity)
		r.Error(err)
		r.ErrorIs(err, credentials.ErrNotFound)
		r.Nil(creds)
	})

	t.Run("concurrent access", func(t *testing.T) {
		var wg sync.WaitGroup
		for i := 0; i < 1000; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				identity := runtime.Identity{
					"type":     "OCIRegistry",
					"hostname": "quay.io",
				}
				creds, err := resolveMap(t, identity)
				r.NoError(err)
				r.Equal("quayuser", creds["username"])
			}()
		}
		wg.Wait()
	})

	t.Run("cloned credentials are independent", func(t *testing.T) {
		identity := runtime.Identity{
			"type":     "OCIRegistry",
			"hostname": "docker.io",
		}
		creds, err := resolveMap(t, identity)
		r.NoError(err)

		creds["username"] = "modifieduser"

		creds2, err := resolveMap(t, identity)
		r.NoError(err)
		r.Equal("testuser", creds2["username"])
	})
}

func TestStaticTypedCredentialsResolver(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	dockerIdentity := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io"}
	quayIdentity := runtime.Identity{"type": "OCIRegistry", "hostname": "quay.io"}

	typed := func(username, password string) *v1.DirectCredentials {
		return &v1.DirectCredentials{
			Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
			Properties: map[string]string{"username": username, "password": password},
		}
	}

	resolver := credentials.NewStaticTypedCredentialsResolver(map[string]runtime.Typed{
		dockerIdentity.String(): typed("dockeruser", "dockerpass"),
		quayIdentity.String():   typed("quayuser", "quaypass"),
	})

	tests := []struct {
		name         string
		identity     runtime.Identity
		wantUsername string
		wantErr      error
	}{
		{
			name:         "resolves docker credentials",
			identity:     dockerIdentity,
			wantUsername: "dockeruser",
		},
		{
			name:         "resolves quay credentials",
			identity:     quayIdentity,
			wantUsername: "quayuser",
		},
		{
			name:    "not found returns ErrNotFound",
			identity: runtime.Identity{"type": "OCIRegistry", "hostname": "unknown.io"},
			wantErr: credentials.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resolver.Resolve(ctx, tt.identity)
			if tt.wantErr != nil {
				r.ErrorIs(err, tt.wantErr)
				return
			}
			r.NoError(err)
			dc := result.(*v1.DirectCredentials)
			r.Equal(tt.wantUsername, dc.Properties["username"])
		})
	}

	t.Run("returned credentials are independent copies", func(t *testing.T) {
		first, err := resolver.Resolve(ctx, dockerIdentity)
		r.NoError(err)
		first.(*v1.DirectCredentials).Properties["username"] = "mutated"

		second, err := resolver.Resolve(ctx, dockerIdentity)
		r.NoError(err)
		r.Equal("dockeruser", second.(*v1.DirectCredentials).Properties["username"])
	})
}
