package integration_test

import (
	"bytes"
	"testing"
	"text/template"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/git/repository"
	"ocm.software/open-component-model/bindings/go/git/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
)

// ocmV1Descriptor is a component descriptor as the OCM v1 CLI 0.51.0 writes it
// for a Git access resource; repository, type and commit are set per case.
const ocmV1Descriptor = `component:
  componentReferences: []
  creationTime: "2026-09-25T08:34:00Z"
  name: acme.org/gitcompat
  provider: acme.org
  repositoryContexts: []
  resources:
  - access:
      {{- if .Commit }}
      commit: {{ .Commit }}
      {{- end }}
      ref: refs/heads/main
      repository: {{ .Repository }}
      type: {{ .Type }}
    name: src
    relation: external
    type: blob
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
`

// Test_Integration_GitOCMv1Compatibility reads Git access resources as OCM v1
// writes them into a component descriptor and downloads them over HTTPS.
// The v1 digest is not part of the descriptor: v1 archives carry the owner of
// the machine that built them, so their digest cannot be reproduced.
//
func Test_Integration_GitOCMv1Compatibility(t *testing.T) {
	path, first := newRepository(t)
	url, ca := newHTTPSServer(t, path, "")
	trustServerCertificate(t, ca)
	tmpl := template.Must(template.New("descriptor").Parse(ocmV1Descriptor))

	// OCM v1 registers these type names for the same Git access.
	for _, typ := range []string{"git", "git/v1alpha1", "Git", "Git/v1alpha1"} {
		for _, revision := range []struct{ name, commit, content string }{
			{"ref", "", "second\n"},
			{"ref and commit", first.String(), "first\n"},
		} {
			t.Run(typ+"/"+revision.name, func(t *testing.T) {
				r := require.New(t)

				var data bytes.Buffer
				r.NoError(tmpl.Execute(&data, map[string]string{"Repository": url, "Type": typ, "Commit": revision.commit}))
				r.NoError(descriptorv2.ValidateRawYAML(data.Bytes()))
				var v2 descriptorv2.Descriptor
				r.NoError(yaml.Unmarshal(data.Bytes(), &v2))
				desc, err := descriptor.ConvertFromV2(&v2)
				r.NoError(err)
				res := &desc.Component.Resources[0]

				tempDir := t.TempDir()
				repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir})
				b, err := repo.DownloadResource(t.Context(), res, nil)
				r.NoError(err)
				archive := assertArchive(t, b, revision.content)

				pinned, err := repo.ProcessResourceDigest(t.Context(), res, nil)
				r.NoError(err)
				r.Equal(digest.FromBytes(archive).Encoded(), pinned.Digest.Value)
				var spec accessv1.Git
				r.NoError(access.Scheme.Convert(pinned.Access, &spec))
				r.Equal("refs/heads/main", spec.Ref)
				if revision.commit != "" {
					r.Equal(revision.commit, spec.Commit)
				} else {
					// Pinned to the tip of main, which is the second commit.
					r.Len(spec.Commit, 40)
					r.NotEqual(first.String(), spec.Commit)
				}
			})
		}
	}
}
