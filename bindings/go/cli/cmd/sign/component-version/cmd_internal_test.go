package componentversion

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/signing/tsa"
)

// labelValue decodes the JSON-encoded TSA URL stored in the signed label for the
// given signature name.
func labelValue(t *testing.T, desc *descruntime.Descriptor, signatureName string) (string, bool) {
	t.Helper()
	name := tsa.TSAURLLabelPrefix + signatureName
	for _, l := range desc.Component.Labels {
		if l.Name != name {
			continue
		}
		var url string
		require.NoError(t, json.Unmarshal(l.Value, &url))
		return url, true
	}
	return "", false
}

func TestAddSignedTSALabel_StripsSensitiveComponents(t *testing.T) {
	r := require.New(t)
	desc := &descruntime.Descriptor{}

	r.NoError(addSignedTSALabel(desc, "default", "https://user:token@tsa.example:8443/ts?apikey=secret#frag"))

	val, ok := labelValue(t, desc, "default")
	r.True(ok)
	// Only scheme/host/port/path survive; userinfo, query and fragment are dropped.
	r.Equal("https://tsa.example:8443/ts", val)
	r.NotContains(val, "token")
	r.NotContains(val, "secret")

	// The label must be signing-relevant so it is covered by the digest.
	name := tsa.TSAURLLabelPrefix + "default"
	for _, l := range desc.Component.Labels {
		if l.Name == name {
			r.True(l.Signing)
		}
	}
}

func TestAddSignedTSALabel_RejectsOpaqueURL(t *testing.T) {
	r := require.New(t)
	desc := &descruntime.Descriptor{}

	// url.Parse treats "user" as the scheme and keeps "pass@..." in Opaque, so
	// URL.String would re-emit the credential-bearing input. Sanitization must
	// refuse it rather than persist credentials.
	err := addSignedTSALabel(desc, "default", "user:pass@tsa.example/ts")
	r.Error(err)
	r.Empty(desc.Component.Labels)
}

func TestAddSignedTSALabel_ReplacesExistingLabel(t *testing.T) {
	r := require.New(t)
	desc := &descruntime.Descriptor{}

	r.NoError(addSignedTSALabel(desc, "default", "https://old.example/ts"))
	r.NoError(addSignedTSALabel(desc, "default", "https://new.example/ts"))

	// A --force re-sign must replace the label in place, not append a duplicate.
	name := tsa.TSAURLLabelPrefix + "default"
	count := 0
	for _, l := range desc.Component.Labels {
		if l.Name == name {
			count++
		}
	}
	r.Equal(1, count)

	val, ok := labelValue(t, desc, "default")
	r.True(ok)
	r.Equal("https://new.example/ts", val)
}
