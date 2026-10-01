package credentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/credentials"
	credentialruntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	gitidentityv1 "ocm.software/open-component-model/bindings/go/git/spec/identity/v1"
	ociidentityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	wgetidentityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

// ocmv1CredentialConfig is a credential config as written for the OCM v1 CLI: consumers are
// scoped by the lowercase pathprefix attribute, which v1 matched segment by segment.
const ocmv1CredentialConfig = `type: credentials.config.ocm.software
consumers:
  - identity:
      type: OCIRegistry
      hostname: ghcr.io
      pathprefix: open-component-model
    credentials:
      - type: Credentials
        properties:
          username: oci-org
  - identity:
      type: Git
      hostname: gitlab.example.com
      scheme: https
      pathprefix: group
    credentials:
      - type: Credentials
        properties:
          username: git-group
  - identity:
      type: Wget
      hostname: files.example.com
      pathprefix: /downloads
    credentials:
      - type: Credentials
        properties:
          username: wget-downloads
`

// TestOCMv1CredentialConfigCompatibility resolves the consumer identities the bindings
// build for a lookup against a credential config written for OCM v1, which scopes its
// consumers by pathprefix.
func TestOCMv1CredentialConfigCompatibility(t *testing.T) {
	r := require.New(t)

	var config v1.Config
	r.NoError(yaml.Unmarshal([]byte(ocmv1CredentialConfig), &config))
	graph, err := credentials.ToGraph(t.Context(), credentialruntime.ConvertFromV1(&config), credentials.Options{})
	r.NoError(err)

	ociIdentity := func(baseURL string) func() (runtime.Identity, error) {
		return func() (runtime.Identity, error) {
			return ociidentityv1.IdentityFromOCIRepository(&oci.Repository{BaseUrl: baseURL})
		}
	}
	gitIdentity := func(url string) func() (runtime.Identity, error) {
		return func() (runtime.Identity, error) { return gitidentityv1.IdentityFromURL(url) }
	}
	wgetIdentity := func(url string) func() (runtime.Identity, error) {
		return func() (runtime.Identity, error) { return wgetidentityv1.IdentityFromURL(url) }
	}

	for _, tc := range []struct {
		name     string
		identity func() (runtime.Identity, error)
		// username is empty when no credentials must be found.
		username string
	}{
		{"oci prefix itself", ociIdentity("https://ghcr.io/open-component-model"), "oci-org"},
		{"oci repository below prefix", ociIdentity("https://ghcr.io/open-component-model/ocm"), "oci-org"},
		{"oci nested repository below prefix", ociIdentity("https://ghcr.io/open-component-model/ocm/charts"), "oci-org"},
		{"oci sibling with the same prefix", ociIdentity("https://ghcr.io/open-component-model-extra/ocm"), ""},
		{"oci other organization", ociIdentity("https://ghcr.io/acme/ocm"), ""},
		{"git repository below prefix", gitIdentity("https://gitlab.example.com/group/repo.git"), "git-group"},
		{"git repository in a subgroup", gitIdentity("https://gitlab.example.com/group/sub/repo.git"), "git-group"},
		{"git other group", gitIdentity("https://gitlab.example.com/other/repo.git"), ""},
		{"git other scheme", gitIdentity("ssh://git@gitlab.example.com/group/repo.git"), ""},
		{"wget file below prefix with leading slash", wgetIdentity("https://files.example.com/downloads/v1/app.tgz"), "wget-downloads"},
		{"wget file outside prefix", wgetIdentity("https://files.example.com/uploads/app.tgz"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			identity, err := tc.identity()
			r.NoError(err)

			resolved, err := graph.Resolve(t.Context(), identity)
			if tc.username == "" {
				r.ErrorIs(err, credentials.ErrNotFound)
				return
			}
			r.NoError(err)
			r.Equal(tc.username, resolved.(*v1.DirectCredentials).Properties["username"])
		})
	}
}
