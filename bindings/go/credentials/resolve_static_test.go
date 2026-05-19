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
