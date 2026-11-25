package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAttestationType(t *testing.T) {
	cases := []struct {
		name string
		att  Attestation
		want string
	}{
		{"predicate wins", Attestation{PredicateType: "https://spdx.dev/Document", ArtifactType: "application/spdx+json", MediaType: "application/spdx+json"}, "https://spdx.dev/Document"},
		{"artifact type when no predicate", Attestation{ArtifactType: "application/vnd.cncf.notary.signature", MediaType: "application/jose+json"}, "application/vnd.cncf.notary.signature"},
		{"media type is the last resort", Attestation{MediaType: "application/openvex+json"}, "application/openvex+json"},
		{"empty when nothing is set", Attestation{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, c.att.Type())
		})
	}
}

func TestAttestationString(t *testing.T) {
	assert.Equal(t, "https://spdx.dev/Document", Attestation{PredicateType: "https://spdx.dev/Document"}.String())
	assert.Equal(t, "application/spdx+json (linux/amd64)",
		Attestation{MediaType: "application/spdx+json", Platform: Platform{OS: "linux", Architecture: "amd64"}}.String())
	assert.Equal(t, "attestation", Attestation{}.String(), "a typeless attestation still names itself without spilling content")
}
