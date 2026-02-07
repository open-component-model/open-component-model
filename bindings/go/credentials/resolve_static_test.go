package credentials_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
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

	t.Run("resolve existing credentials", func(t *testing.T) {
		identity := runtime.Identity{
			"type":     "OCIRegistry",
			"hostname": "docker.io",
		}
		creds, err := resolver.Resolve(context.Background(), identity)
		r.NoError(err)
		r.Equal("testuser", creds["username"])
		r.Equal("testpass", creds["password"])
	})

	t.Run("resolve not found", func(t *testing.T) {
		identity := runtime.Identity{
			"type":     "OCIRegistry",
			"hostname": "notfound.io",
		}
		creds, err := resolver.Resolve(context.Background(), identity)
		r.Error(err)
		r.ErrorIs(err, credentials.ErrNotFound)
		r.Nil(creds)
	})

	t.Run("concurrent access", func(t *testing.T) {
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				identity := runtime.Identity{
					"type":     "OCIRegistry",
					"hostname": "quay.io",
				}
				creds, err := resolver.Resolve(context.Background(), identity)
				r.NoError(err)
				r.Equal("quayuser", creds["username"])
			}()
		}
		wg.Wait()
	})
}
