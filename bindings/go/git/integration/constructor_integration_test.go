package integration_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/ctf"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	gitinput "ocm.software/open-component-model/bindings/go/git/input"
	inputspec "ocm.software/open-component-model/bindings/go/git/spec/input"
	inputv1 "ocm.software/open-component-model/bindings/go/git/spec/input/v1"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
)

func Test_Integration_GitInputConstruction(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	path, _ := newRepository(t)
	url, ca := newHTTPSServer(t, path, "")

	trustServerCertificate(t, ca)
	method := &gitinput.InputMethod{TempFolder: t.TempDir()}
	inputs := constructor.New(inputspec.Scheme)
	r.NoError(inputs.RegisterResourceInputMethod(&inputv1.Git{}, method))
	construct := func(t *testing.T, typ string, expected *constructorv1.Digest) (*oci.Repository, error) {
		t.Helper()
		r := require.New(t)
		fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
		r.NoError(err)
		repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
		r.NoError(err)

		// Omitting both selectors must archive remote HEAD, not the first commit.
		var spec constructorv1.ComponentConstructor
		r.NoError(yaml.Unmarshal([]byte(fmt.Sprintf(`
components:
  - name: ocm.software/git-app
    version: 1.0.0
    provider:
      name: ocm
    resources:
      - name: source-archive
        version: 1.0.0
        relation: local
        type: blob
        input:
          type: %s
          repository: %s
`, typ, url)), &spec))
		spec.Components[0].Resources[0].Digest = expected
		err = constructor.NewDefaultConstructor(
			constructorruntime.ConvertToRuntimeConstructor(&spec),
			constructor.Options{
				ResourceInputMethodProvider: inputs,
				TargetRepositoryProvider:    gitTargetRepositoryProvider{repo: repo},
			},
		).Construct(t.Context())
		return repo, err
	}

	repo, err := construct(t, "git/v1", nil)
	r.NoError(err)

	desc, err := repo.GetComponentVersion(ctx, "ocm.software/git-app", "1.0.0")
	r.NoError(err)
	r.Len(desc.Component.Resources, 1)
	resource := desc.Component.Resources[0]
	r.Equal("source-archive", resource.Name)
	r.NotNil(resource.Access)
	r.Equal(v2.LocalBlobAccessType, resource.Access.GetType().Name)

	content, _, err := repo.GetLocalResource(ctx, desc.Component.Name, desc.Component.Version, resource.ToIdentity())
	r.NoError(err)
	data := assertArchive(t, content, "second\n")
	r.NotNil(resource.Digest)
	r.Equal("SHA-256", resource.Digest.HashAlgorithm)
	r.Equal("genericBlobDigest/v1", resource.Digest.NormalisationAlgorithm)
	r.Equal(digest.FromBytes(data).Encoded(), resource.Digest.Value)

	for _, tc := range []struct {
		name, typ, hash, normalization, value, wantErr string
	}{
		{"canonical", "git/v1", "SHA-256", "genericBlobDigest/v1", resource.Digest.Value, ""},
		{"unversioned", "git", "SHA-256", "genericBlobDigest/v1", resource.Digest.Value, ""},
		{"legacy uppercase", "Git", "SHA-256", "genericBlobDigest/v1", resource.Digest.Value, ""},
		{"legacy uppercase versioned", "Git/v1", "SHA-256", "genericBlobDigest/v1", resource.Digest.Value, ""},
		{"wrong value", "git/v1", "SHA-256", "genericBlobDigest/v1", strings.Repeat("0", 64), "digest mismatch"},
		{"missing value", "git/v1", "SHA-256", "genericBlobDigest/v1", "", "digest"},
		{"short value", "git/v1", "SHA-256", "genericBlobDigest/v1", "abc", "digest"},
		{"unknown hash", "git/v1", "bogus", "genericBlobDigest/v1", resource.Digest.Value, "hash algorithm"},
		{"missing hash", "git/v1", "", "genericBlobDigest/v1", resource.Digest.Value, "hash algorithm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			_, err := construct(t, tc.typ, &constructorv1.Digest{
				HashAlgorithm: tc.hash, NormalisationAlgorithm: tc.normalization, Value: tc.value,
			})
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
			} else {
				r.NoError(err)
			}
		})
	}
}

type gitTargetRepositoryProvider struct {
	repo constructor.TargetRepository
}

func (p gitTargetRepositoryProvider) GetTargetRepository(_ context.Context, _ *constructorruntime.Component) (constructor.TargetRepository, error) {
	return p.repo, nil
}
