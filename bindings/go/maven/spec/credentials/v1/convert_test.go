package v1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	directcredsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func directCredentials(properties map[string]string) *directcredsv1.DirectCredentials {
	return &directcredsv1.DirectCredentials{
		Type:       runtime.NewVersionedType(directcredsv1.CredentialsType, directcredsv1.Version),
		Properties: properties,
	}
}

func TestConvertToMavenCredentials(t *testing.T) {
	versioned := runtime.NewVersionedType(MavenCredentialsType, Version)

	t.Run("nil credentials convert to nil without an error", func(t *testing.T) {
		converted, err := ConvertToMavenCredentials(nil)
		require.NoError(t, err)
		assert.Nil(t, converted, "most maven repositories are readable anonymously, so absent credentials are not an error")
	})

	t.Run("credentials with an empty type convert to nil without an error", func(t *testing.T) {
		converted, err := ConvertToMavenCredentials(&MavenCredentials{})
		require.NoError(t, err)
		assert.Nil(t, converted)
	})

	t.Run("typed maven credentials pass through", func(t *testing.T) {
		converted, err := ConvertToMavenCredentials(&MavenCredentials{
			Type: versioned, Username: "alice", Password: "s3cr3t", IdentityToken: "tok",
		})
		require.NoError(t, err)
		require.NotNil(t, converted)
		assert.Equal(t, "alice", converted.Username)
		assert.Equal(t, "s3cr3t", converted.Password)
		assert.Equal(t, "tok", converted.IdentityToken)
	})

	t.Run("unversioned alias is accepted", func(t *testing.T) {
		converted, err := ConvertToMavenCredentials(&MavenCredentials{
			Type: runtime.NewUnversionedType(MavenCredentialsType), Username: "alice",
		})
		require.NoError(t, err)
		require.NotNil(t, converted)
		assert.Equal(t, "alice", converted.Username)
	})

	t.Run("direct credentials map username, password and identityToken", func(t *testing.T) {
		converted, err := ConvertToMavenCredentials(directCredentials(map[string]string{
			"username": "alice", "password": "s3cr3t", "identityToken": "tok",
		}))
		require.NoError(t, err)
		require.NotNil(t, converted)
		assert.Equal(t, versioned, converted.Type)
		assert.Equal(t, "alice", converted.Username)
		assert.Equal(t, "s3cr3t", converted.Password)
		assert.Equal(t, "tok", converted.IdentityToken)
	})

	t.Run("direct credentials accept old OCM's accessToken key", func(t *testing.T) {
		converted, err := ConvertToMavenCredentials(directCredentials(map[string]string{"accessToken": "legacy"}))
		require.NoError(t, err)
		require.NotNil(t, converted)
		assert.Equal(t, "legacy", converted.IdentityToken)
	})

	t.Run("identityToken wins over accessToken in a property bag", func(t *testing.T) {
		converted, err := ConvertToMavenCredentials(directCredentials(map[string]string{"identityToken": "new", "accessToken": "legacy"}))
		require.NoError(t, err)
		assert.Equal(t, "new", converted.IdentityToken)
	})

	t.Run("direct credentials without any maven key convert to nil", func(t *testing.T) {
		for name, props := range map[string]map[string]string{
			"nil properties": nil,
			"empty bag":      {},
			"unrelated keys": {"certificate": "pem"},
		} {
			converted, err := ConvertToMavenCredentials(directCredentials(props))
			require.NoError(t, err, name)
			assert.Nil(t, converted, name)
		}
	})

	t.Run("direct credentials with only a password stay present so the client can reject them", func(t *testing.T) {
		converted, err := ConvertToMavenCredentials(directCredentials(map[string]string{"password": "s3cr3t"}))
		require.NoError(t, err)
		require.NotNil(t, converted)
		assert.Empty(t, converted.Username)
		assert.Empty(t, converted.IdentityToken)
	})

	t.Run("unknown credential types are rejected", func(t *testing.T) {
		_, err := ConvertToMavenCredentials(&runtime.Raw{Type: runtime.NewVersionedType("Unknown", "v1"), Data: []byte(`{"type":"Unknown/v1"}`)})
		require.Error(t, err)
	})
}

func TestMavenCredentials_JSONRoundTrip(t *testing.T) {
	in := MavenCredentials{
		Type:          runtime.NewVersionedType(MavenCredentialsType, Version),
		Username:      "alice",
		Password:      "s3cr3t",
		IdentityToken: "tok",
	}
	data, err := json.Marshal(in)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"MavenCredentials/v1","username":"alice","password":"s3cr3t","identityToken":"tok"}`, string(data))

	var out MavenCredentials
	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, in, out)

	data, err = json.Marshal(MavenCredentials{Type: in.Type, IdentityToken: "tok"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"MavenCredentials/v1","identityToken":"tok"}`, string(data), "unset fields are omitted")
}
