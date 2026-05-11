package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestFromDirectCredentials(t *testing.T) {
	tests := []struct {
		name       string
		properties map[string]string
		expected   *OCICredentials
	}{
		{
			name: "all fields populated",
			properties: map[string]string{
				"username":     "myuser",
				"password":     "mypass",
				"accessToken":  "token123",
				"refreshToken": "refresh456",
			},
			expected: &OCICredentials{
				Type:         runtime.NewVersionedType(OCICredentialsType, Version),
				Username:     "myuser",
				Password:     "mypass",
				AccessToken:  "token123",
				RefreshToken: "refresh456",
			},
		},
		{
			name: "only username and password",
			properties: map[string]string{
				"username": "myuser",
				"password": "mypass",
			},
			expected: &OCICredentials{
				Type:     runtime.NewVersionedType(OCICredentialsType, Version),
				Username: "myuser",
				Password: "mypass",
			},
		},
		{
			name:       "empty properties",
			properties: map[string]string{},
			expected: &OCICredentials{
				Type: runtime.NewVersionedType(OCICredentialsType, Version),
			},
		},
		{
			name: "ignores unknown properties",
			properties: map[string]string{
				"username":    "myuser",
				"unknownProp": "value",
			},
			expected: &OCICredentials{
				Type:     runtime.NewVersionedType(OCICredentialsType, Version),
				Username: "myuser",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FromDirectCredentials(tt.properties)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMustRegisterCredentialType(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	// Should resolve versioned type
	obj, err := scheme.NewObject(runtime.NewVersionedType(OCICredentialsType, Version))
	require.NoError(t, err)
	assert.IsType(t, &OCICredentials{}, obj)

	// Should resolve unversioned alias
	obj, err = scheme.NewObject(runtime.NewUnversionedType(OCICredentialsType))
	require.NoError(t, err)
	assert.IsType(t, &OCICredentials{}, obj)
}

func TestOCICredentials_SchemeConvert(t *testing.T) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	MustRegisterCredentialType(scheme)

	original := &OCICredentials{
		Type:         runtime.NewVersionedType(OCICredentialsType, Version),
		Username:     "testuser",
		Password:     "testpass",
		AccessToken:  "tok",
		RefreshToken: "ref",
	}

	raw := &runtime.Raw{}
	require.NoError(t, scheme.Convert(original, raw))

	restored := &OCICredentials{}
	require.NoError(t, scheme.Convert(raw, restored))

	assert.Equal(t, original.Type, restored.Type)
	assert.Equal(t, original.Username, restored.Username)
	assert.Equal(t, original.Password, restored.Password)
	assert.Equal(t, original.AccessToken, restored.AccessToken)
	assert.Equal(t, original.RefreshToken, restored.RefreshToken)
}
