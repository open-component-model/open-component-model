package integration_test

import (
	"context"
	"fmt"
	"os"
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
          type: git/v1
          repository: %s
`, url)), &spec))

	inputs := constructor.New(inputspec.Scheme)
	r.NoError(inputs.RegisterResourceInputMethod(&inputv1.Git{}, &gitinput.InputMethod{
		TempFolder: t.TempDir(),
		CABundle:   ca,
	}))
	r.NoError(constructor.NewDefaultConstructor(
		constructorruntime.ConvertToRuntimeConstructor(&spec),
		constructor.Options{
			ResourceInputMethodProvider: inputs,
			TargetRepositoryProvider:    gitTargetRepositoryProvider{repo: repo},
		},
	).Construct(ctx))

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
}

type gitTargetRepositoryProvider struct {
	repo constructor.TargetRepository
}

func (p gitTargetRepositoryProvider) GetTargetRepository(_ context.Context, _ *constructorruntime.Component) (constructor.TargetRepository, error) {
	return p.repo, nil
}
