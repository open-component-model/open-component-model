package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestCanonicalHashV1(t *testing.T) {
	t.Run("Empty Identity", func(t *testing.T) {
		identity := runtime.Identity{}
		hash := identity.CanonicalHashV1()
		assert.NotZero(t, hash, "Hash of empty identity should not be zero")
	})

	t.Run("Single Key-Value Pair", func(t *testing.T) {
		identity := runtime.Identity{"key": "value"}
		hash := identity.CanonicalHashV1()
		assert.NotZero(t, hash, "Hash should not be zero")
	})

	t.Run("Multiple Key-Value Pairs", func(t *testing.T) {
		identity := runtime.Identity{"a": "1", "b": "2", "c": "3"}
		hash := identity.CanonicalHashV1()
		assert.NotZero(t, hash, "Hash should not be zero")
	})

	t.Run("Order Stability", func(t *testing.T) {
		h1 := runtime.Identity{"a": "1", "b": "2", "c": "3"}.CanonicalHashV1()
		h2 := runtime.Identity{"c": "3", "b": "2", "a": "1"}.CanonicalHashV1() // Different order

		assert.Equal(t, h1, h2, "Hashes should be the same regardless of key order")
	})

	t.Run("Different Values Produce Different Hashes", func(t *testing.T) {
		h1 := runtime.Identity{"a": "1", "b": "2"}.CanonicalHashV1()
		h2 := runtime.Identity{"a": "1", "b": "3"}.CanonicalHashV1() // Different value for 'b'

		assert.NotEqual(t, h1, h2, "Hashes should be different when values are different")
	})
}

func TestParseURLToIdentity(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    runtime.Identity
		wantErr bool
	}{
		{
			uri: "http://docker.io",
			want: runtime.Identity{
				runtime.IdentityAttributeHostname: "docker.io",
				runtime.IdentityAttributeScheme:   "http",
			},
		},
		{
			uri: "https://docker.io",
			want: runtime.Identity{
				runtime.IdentityAttributeHostname: "docker.io",
				runtime.IdentityAttributeScheme:   "https",
			},
		},
		{
			uri: "docker.io",
			want: runtime.Identity{
				runtime.IdentityAttributeHostname: "docker.io",
			},
		},
		{
			uri: "my-registry.io:5000",
			want: runtime.Identity{
				runtime.IdentityAttributeHostname: "my-registry.io",
				runtime.IdentityAttributePort:     "5000",
			},
		},
		{
			uri: "my-registry.io:5000/path",
			want: runtime.Identity{
				runtime.IdentityAttributeHostname: "my-registry.io",
				runtime.IdentityAttributePort:     "5000",
				runtime.IdentityAttributePath:     "path",
			},
		},
		{
			uri: "localhost:8080",
			want: runtime.Identity{
				runtime.IdentityAttributeHostname: "localhost",
				runtime.IdentityAttributePort:     "8080",
			},
		},
		{
			uri: "plain-host",
			want: runtime.Identity{
				runtime.IdentityAttributeHostname: "plain-host",
			},
		},
		{
			uri: "http://",
			want: runtime.Identity{
				runtime.IdentityAttributeScheme: "http",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			r := require.New(t)
			got, err := runtime.ParseURLToIdentity(tt.uri)
			if tt.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.Truef(tt.want.Equal(got), "expected %v to be equal to %v", tt.want, got)
		})
	}
}
