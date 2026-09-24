package componentversion

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing/tsa"
)

func mustRaw(t *testing.T, jsonStr string) *runtime.Raw {
	t.Helper()
	raw := &runtime.Raw{}
	require.NoError(t, json.Unmarshal([]byte(jsonStr), raw))
	return raw
}

func TestCredentialProperties_DirectCredentials(t *testing.T) {
	r := require.New(t)
	dc := &credconfigv1.DirectCredentials{
		Type:       runtime.NewVersionedType(credconfigv1.CredentialsType, credconfigv1.Version),
		Properties: map[string]string{"public_key_pem": "KEY", "extra": "x"},
	}
	props := credentialProperties(dc)
	r.Equal("KEY", props["public_key_pem"])
	r.Equal("x", props["extra"])
}

func TestCredentialProperties_RawWithNestedProperties(t *testing.T) {
	r := require.New(t)
	// A plugin-resolved credential arriving as *runtime.Raw that wraps
	// DirectCredentials: the material lives under a nested "properties" object.
	raw := mustRaw(t, `{"type":"Credentials/v1","properties":{"public_key_pem":"PUBKEY","private_key_pem":"PRIVKEY"}}`)
	props := credentialProperties(raw)
	r.Equal("PUBKEY", props["public_key_pem"], "nested properties must be preserved, not dropped")
	r.Equal("PRIVKEY", props["private_key_pem"])
}

func TestCredentialProperties_RawWithFlatFields(t *testing.T) {
	r := require.New(t)
	// A typed credential (flat string fields) arriving as *runtime.Raw.
	raw := mustRaw(t, `{"type":"RSA/v1alpha1","publicKeyPEM":"PUBKEY"}`)
	props := credentialProperties(raw)
	r.Equal("PUBKEY", props["publicKeyPEM"])
	r.NotContains(props, "type")
}

func TestWithVerifiedTime_PreservesMaterialAndAddsTime(t *testing.T) {
	r := require.New(t)
	raw := mustRaw(t, `{"type":"Credentials/v1","properties":{"public_key_pem":"PUBKEY"}}`)
	ts := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

	out := withVerifiedTime(raw, ts)
	dc, ok := out.(*credconfigv1.DirectCredentials)
	r.True(ok)
	r.Equal("PUBKEY", dc.Properties["public_key_pem"], "credential material must survive re-wrapping")
	r.Equal(ts.Format(time.RFC3339), dc.Properties[tsa.VerifiedTimeKey])
}
