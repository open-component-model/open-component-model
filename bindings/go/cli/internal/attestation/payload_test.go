package attestation

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/repository"
)

func TestPayload(t *testing.T) {
	predicate := `{"vulns":["CVE-1"],"status":"fixed"}`
	statement := `{"_type":"https://in-toto.io/Statement/v1","predicateType":"https://openvex.dev/ns/v0.2.0","subject":[],"predicate":` + predicate + `}`
	dsseEnvelope := func(stmt string) string {
		return `{"payload":"` + base64.StdEncoding.EncodeToString([]byte(stmt)) + `","payloadType":"application/vnd.in-toto+json","signatures":[{"sig":"abc"}]}`
	}
	bareDoc := `{"@context":"https://openvex.dev/ns/v0.2.0","statements":[]}`

	tests := []struct {
		name string
		data string
		want string // empty means ok=false
	}{
		{
			name: "in-toto statement yields its predicate",
			data: statement,
			want: predicate,
		},
		{
			name: "sigstore bundle unwraps the DSSE envelope to the predicate",
			data: `{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json","verificationMaterial":{},"dsseEnvelope":` + dsseEnvelope(statement) + `}`,
			want: predicate,
		},
		{
			name: "bare DSSE envelope unwraps to the predicate",
			data: dsseEnvelope(statement),
			want: predicate,
		},
		{
			name: "document with no envelope is returned unchanged",
			data: bareDoc,
			want: bareDoc,
		},
		{
			name: "DSSE envelope wrapping a non-statement returns the decoded payload",
			data: `{"payload":"` + base64.StdEncoding.EncodeToString([]byte(bareDoc)) + `","payloadType":"application/vnd.in-toto+json"}`,
			want: bareDoc,
		},
		{
			name: "non-JSON bytes have no payload",
			data: "\x00\x01notation-signature",
			want: "",
		},
		{
			name: "empty document has no payload",
			data: "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Payload(repository.Attestation{Data: []byte(tt.data)})
			if tt.want == "" {
				assert.False(t, ok, "expected no extractable payload")
				assert.Nil(t, got)
				return
			}
			require.True(t, ok, "expected an extractable payload")
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}

// TestPayload_RawStdEncoding covers producers that emit an unpadded base64 DSSE payload.
func TestPayload_RawStdEncoding(t *testing.T) {
	predicate := `{"k":"v"}`
	statement := `{"_type":"https://in-toto.io/Statement/v1","predicate":` + predicate + `}`
	envelope := `{"payload":"` + base64.RawStdEncoding.EncodeToString([]byte(statement)) + `","payloadType":"application/vnd.in-toto+json"}`

	got, ok := Payload(repository.Attestation{Data: []byte(envelope)})
	require.True(t, ok)
	assert.JSONEq(t, predicate, string(got))
}

// ensure the decoder output is itself valid JSON for every ok result, so callers can
// re-serialize without re-checking.
func TestPayload_OutputIsValidJSON(t *testing.T) {
	statement := `{"_type":"https://in-toto.io/Statement/v1","predicate":{"a":1}}`
	got, ok := Payload(repository.Attestation{Data: []byte(statement)})
	require.True(t, ok)
	assert.True(t, got.IsValid())
}
