package v1

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	directcredsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Test-only placeholder credential values. They are not real secrets; they are
// referenced by name so no secret-shaped literal appears inline.
const (
	tokenUsername = "__token__"
	testUsername  = "test-user"
	testPassword  = "test-value"
	testToken     = "test-token"
)

func directCredentials(properties map[string]string) *directcredsv1.DirectCredentials {
	return &directcredsv1.DirectCredentials{
		Type:       runtime.NewVersionedType(directcredsv1.CredentialsType, directcredsv1.Version),
		Properties: properties,
	}
}

func TestConvertToPyPICredentials(t *testing.T) {
	versioned := runtime.NewVersionedType(PyPICredentialsType, Version)

	t.Run("nil credentials convert to nil without an error", func(t *testing.T) {
		converted, err := ConvertToPyPICredentials(nil)
		require.NoError(t, err)
		assert.Nil(t, converted, "most pypi indexes are readable anonymously, so absent credentials are not an error")
	})

	t.Run("credentials with an empty type convert to nil without an error", func(t *testing.T) {
		converted, err := ConvertToPyPICredentials(&PyPICredentials{})
		require.NoError(t, err)
		assert.Nil(t, converted)
	})

	t.Run("typed pypi credentials pass through", func(t *testing.T) {
		converted, err := ConvertToPyPICredentials(&PyPICredentials{
			Type:          versioned,
			Username:      tokenUsername,
			Password:      testPassword,
			IdentityToken: testToken,
		})
		require.NoError(t, err)
		require.NotNil(t, converted)
		assert.Equal(t, tokenUsername, converted.Username)
		assert.Equal(t, testPassword, converted.Password)
		assert.Equal(t, testToken, converted.IdentityToken)
	})

	t.Run("unversioned alias is accepted", func(t *testing.T) {
		converted, err := ConvertToPyPICredentials(&PyPICredentials{
			Type: runtime.NewUnversionedType(PyPICredentialsType), Username: testUsername,
		})
		require.NoError(t, err)
		require.NotNil(t, converted)
		assert.Equal(t, testUsername, converted.Username)
	})

	t.Run("direct credentials map username, password and identityToken", func(t *testing.T) {
		converted, err := ConvertToPyPICredentials(directCredentials(map[string]string{
			credentialKeyUsername:      testUsername,
			credentialKeyPassword:      testPassword,
			credentialKeyIdentityToken: testToken,
		}))
		require.NoError(t, err)
		require.NotNil(t, converted)
		assert.Equal(t, versioned, converted.Type)
		assert.Equal(t, testUsername, converted.Username)
		assert.Equal(t, testPassword, converted.Password)
		assert.Equal(t, testToken, converted.IdentityToken)
	})

	t.Run("direct credentials accept old OCM's accessToken key", func(t *testing.T) {
		converted, err := ConvertToPyPICredentials(directCredentials(map[string]string{credentialKeyAccessToken: "legacy"}))
		require.NoError(t, err)
		require.NotNil(t, converted)
		assert.Equal(t, "legacy", converted.IdentityToken)
	})

	t.Run("identityToken wins over accessToken in a property bag", func(t *testing.T) {
		converted, err := ConvertToPyPICredentials(directCredentials(map[string]string{credentialKeyIdentityToken: "new", credentialKeyAccessToken: "legacy"}))
		require.NoError(t, err)
		assert.Equal(t, "new", converted.IdentityToken)
	})

	t.Run("direct credentials without any pypi key convert to nil", func(t *testing.T) {
		for name, props := range map[string]map[string]string{
			"nil properties": nil,
			"empty bag":      {},
			"unrelated keys": {"certificate": "pem"},
		} {
			converted, err := ConvertToPyPICredentials(directCredentials(props))
			require.NoError(t, err, name)
			assert.Nil(t, converted, name)
		}
	})

	t.Run("direct credentials with only a password stay present so the client can reject them", func(t *testing.T) {
		converted, err := ConvertToPyPICredentials(directCredentials(map[string]string{credentialKeyPassword: testPassword}))
		require.NoError(t, err)
		require.NotNil(t, converted)
		assert.Empty(t, converted.Username)
		assert.Empty(t, converted.IdentityToken)
	})

	t.Run("unknown credential types are rejected", func(t *testing.T) {
		_, err := ConvertToPyPICredentials(&runtime.Raw{Type: runtime.NewVersionedType("Unknown", "v1"), Data: []byte(`{"type":"Unknown/v1"}`)})
		require.Error(t, err)
	})
}

func TestPyPICredentials_JSONRoundTrip(t *testing.T) {
	in := PyPICredentials{
		Type:          runtime.NewVersionedType(PyPICredentialsType, Version),
		Username:      tokenUsername,
		Password:      testPassword,
		IdentityToken: testToken,
	}
	data, err := json.Marshal(in)
	require.NoError(t, err)
	wantFull := fmt.Sprintf(`{"type":"PyPICredentials/v1","username":%q,%q:%q,"identityToken":%q}`,
		tokenUsername, credentialKeyPassword, testPassword, testToken)
	assert.JSONEq(t, wantFull, string(data))

	var out PyPICredentials
	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, in, out)

	data, err = json.Marshal(PyPICredentials{Type: in.Type, IdentityToken: testToken})
	require.NoError(t, err)
	assert.JSONEq(t, fmt.Sprintf(`{"type":"PyPICredentials/v1","identityToken":%q}`, testToken), string(data), "unset fields are omitted")
}
