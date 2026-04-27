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

func TestParseIdentity(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expected      runtime.Identity
		expectedError string
	}{
		{
			name:     "valid single key-value pair",
			input:    "key=value",
			expected: runtime.Identity{"key": "value"},
		},
		{
			name:     "valid multiple key-value pairs",
			input:    "key1=value1,key2=value2",
			expected: runtime.Identity{"key1": "value1", "key2": "value2"},
		},
		{
			name:     "valid with whitespace",
			input:    " key1 = value1 , key2 = value2 ",
			expected: runtime.Identity{"key1": "value1", "key2": "value2"},
		},
		{
			name:          "empty input",
			expectedError: "invalid identity part \"\"",
		},
		{
			name:          "missing value",
			input:         "key=",
			expectedError: "invalid identity part \"key=\"",
		},
		{
			name:          "missing key",
			input:         "=value",
			expectedError: "invalid identity part \"=value\"",
		},
		{
			name:          "invalid format - no equals sign",
			input:         "keyvalue",
			expectedError: "invalid identity part \"keyvalue\"",
		},
		{
			name:     "valid with multiple equals in value",
			input:    "key=value=with=equals",
			expected: runtime.Identity{"key": "value=with=equals"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := runtime.ParseIdentity(tt.input)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestIdentityString(t *testing.T) {
	tests := []struct {
		name     string
		identity runtime.Identity
		expected string
	}{
		{
			name:     "empty identity",
			identity: runtime.Identity{},
			expected: "",
		},
		{
			name:     "single key-value pair",
			identity: runtime.Identity{"key": "value"},
			expected: "key=value",
		},
		{
			name:     "multiple key-value pairs",
			identity: runtime.Identity{"b": "2", "a": "1", "c": "3"},
			expected: "a=1,b=2,c=3",
		},
		{
			name:     "special characters in values",
			identity: runtime.Identity{"key1": "value,with,commas", "key2": "value=with=equals"},
			expected: "key1=value,with,commas,key2=value=with=equals",
		},
		{
			name:     "whitespace in values",
			identity: runtime.Identity{"key": "value with spaces"},
			expected: "key=value with spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.identity.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}
