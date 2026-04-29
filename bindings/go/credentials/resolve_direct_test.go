package credentials

import (
	"testing"

	"github.com/stretchr/testify/assert"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// testTypedCredential is a typed credential struct for testing typedToMap.
type testTypedCredential struct {
	Type     runtime.Type `json:"type"`
	Username string       `json:"username,omitempty"`
	Password string       `json:"password,omitempty"`
	CertFile string       `json:"certFile,omitempty"`
}

func (t *testTypedCredential) GetType() runtime.Type    { return t.Type }
func (t *testTypedCredential) SetType(typ runtime.Type) { t.Type = typ }
func (t *testTypedCredential) DeepCopyTyped() runtime.Typed {
	cp := *t
	return &cp
}

func Test_typedToMap_nil(t *testing.T) {
	assert.Nil(t, typedToMap(nil))
}

func Test_typedToMap_DirectCredentials(t *testing.T) {
	dc := &v1.DirectCredentials{
		Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Properties: map[string]string{"username": "admin", "password": "secret"},
	}
	result := typedToMap(dc)
	assert.Equal(t, map[string]string{"username": "admin", "password": "secret"}, result)
}

func Test_typedToMap_TypedCredential_JSONFallback(t *testing.T) {
	cred := &testTypedCredential{
		Type:     runtime.NewVersionedType("TestCreds", "v1"),
		Username: "user",
		Password: "pass",
		CertFile: "/path/to/cert",
	}
	result := typedToMap(cred)
	assert.Equal(t, "user", result["username"])
	assert.Equal(t, "pass", result["password"])
	assert.Equal(t, "/path/to/cert", result["certFile"])
	// type field should be excluded
	assert.Empty(t, result["type"])
}

func Test_typedToMap_TypedCredential_OmitsEmptyFields(t *testing.T) {
	cred := &testTypedCredential{
		Type:     runtime.NewVersionedType("TestCreds", "v1"),
		Username: "user",
		// Password and CertFile are empty
	}
	result := typedToMap(cred)
	assert.Equal(t, "user", result["username"])
	assert.NotContains(t, result, "password")
	assert.NotContains(t, result, "certFile")
}

func Test_typedToMap_TypedCredential_AllEmpty(t *testing.T) {
	cred := &testTypedCredential{
		Type: runtime.NewVersionedType("TestCreds", "v1"),
	}
	result := typedToMap(cred)
	assert.Nil(t, result, "should return nil when all credential fields are empty")
}
