package credentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/credentials"
	credentialruntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
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

// TestOCMv1CredentialConfigCompatibility resolves consumer identities, shaped as the OCI,
// Git and Wget bindings build them for a lookup, against a credential config written for
// OCM v1, which scopes its consumers by pathprefix. The identities are literals because
// credentials must not import other bindings (see golangci.yml depguard).
func TestOCMv1CredentialConfigCompatibility(t *testing.T) {
	r := require.New(t)

	var config v1.Config
	r.NoError(yaml.Unmarshal([]byte(ocmv1CredentialConfig), &config))
	graph, err := credentials.ToGraph(t.Context(), credentialruntime.ConvertFromV1(&config), credentials.Options{})
	r.NoError(err)

	ociIdentity := func(path string) runtime.Identity {
		return runtime.Identity{"type": "OCIRegistry", "scheme": "https", "hostname": "ghcr.io", "path": path}
	}
	gitIdentity := func(scheme, port, path string) runtime.Identity {
		return runtime.Identity{"type": "Git", "scheme": scheme, "hostname": "gitlab.example.com", "port": port, "path": path}
	}
	wgetIdentity := func(path string) runtime.Identity {
		return runtime.Identity{"type": "Wget", "scheme": "https", "hostname": "files.example.com", "path": path}
	}

	for _, tc := range []struct {
		name     string
		identity runtime.Identity
		// username is empty when no credentials must be found.
		username string
	}{
		{"oci prefix itself", ociIdentity("open-component-model"), "oci-org"},
		{"oci repository below prefix", ociIdentity("open-component-model/ocm"), "oci-org"},
		{"oci nested repository below prefix", ociIdentity("open-component-model/ocm/charts"), "oci-org"},
		{"oci sibling with the same prefix", ociIdentity("open-component-model-extra/ocm"), ""},
		{"oci other organization", ociIdentity("acme/ocm"), ""},
		{"git repository below prefix", gitIdentity("https", "443", "group/repo.git"), "git-group"},
		{"git repository in a subgroup", gitIdentity("https", "443", "group/sub/repo.git"), "git-group"},
		{"git other group", gitIdentity("https", "443", "other/repo.git"), ""},
		{"git other scheme", gitIdentity("ssh", "22", "group/repo.git"), ""},
		{"wget file below prefix with leading slash", wgetIdentity("downloads/v1/app.tgz"), "wget-downloads"},
		{"wget file outside prefix", wgetIdentity("uploads/app.tgz"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			resolved, err := graph.Resolve(t.Context(), tc.identity)
			if tc.username == "" {
				r.ErrorIs(err, credentials.ErrNotFound)
				return
			}
			r.NoError(err)
			r.Equal(tc.username, resolved.(*v1.DirectCredentials).Properties["username"])
		})
	}
}
