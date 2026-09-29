package integration_test

import (
	"os"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	filesystemspec "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	gitrepository "ocm.software/open-component-model/bindings/go/git/repository"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfspec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferspec "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

func Test_Integration_GitAccessTransferByValue(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	path, first := newRepository(t)
	url, ca := newHTTPSServer(t, path, "")
	trustServerCertificate(t, ca)
	temp := t.TempDir()
	resourceRepo := gitrepository.NewResourceRepository(&filesystemspec.Config{TempFolder: &temp})
	processed, err := resourceRepo.ProcessResourceDigest(ctx, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "archive", Version: "1.0.0"}},
		Type:        "directoryTree", Relation: descriptor.ExternalRelation,
		Access: &accessv1.Git{Type: runtime.NewVersionedType(accessv1.Type, accessv1.Version), Repository: url, Ref: "refs/heads/main", Commit: first.String()},
	}, nil)
	r.NoError(err)
	r.NotNil(processed.Digest)
	sourcePath, targetPath := t.TempDir(), t.TempDir()
	openCTF := func(path string) *oci.Repository {
		fs, err := filesystem.NewFS(path, os.O_RDWR)
		r.NoError(err)
		repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
		r.NoError(err)
		return repo
	}
	source := openCTF(sourcePath)
	const name, version = "ocm.software/git-transfer", "1.0.0"
	r.NoError(source.AddComponentVersion(ctx, &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: name, Version: version}},
			Provider:      descriptor.Provider{Name: "ocm.software"}, Resources: []descriptor.Resource{*processed},
		},
	}))
	spec := func(path string) *ctfspec.Repository {
		return &ctfspec.Repository{Type: runtime.NewVersionedType(ctfspec.Type, ctfspec.Version), FilePath: path, AccessMode: ctfspec.AccessModeReadWrite}
	}
	definition, err := transfer.BuildGraphDefinition(ctx,
		&transferspec.Config{CopyMode: transferspec.CopyModeAllResources}, nil,
		transfer.Mapping{Components: []transfer.ComponentID{{Component: name, Version: version}}, Target: spec(targetPath), Resolver: transfer.NewRepositoryResolver(source, spec(sourcePath))},
	)
	r.NoError(err)
	builder := transfer.NewDefaultBuilder(provider.NewComponentVersionRepositoryProvider(), resourceRepo, credentials.NewStaticCredentialsResolver(nil))
	graph, err := builder.BuildAndCheck(definition)
	r.NoError(err)
	r.NoError(graph.Process(ctx))
	target := openCTF(targetPath)
	desc, err := target.GetComponentVersion(ctx, name, version)
	r.NoError(err)
	r.Len(desc.Component.Resources, 1)
	res := desc.Component.Resources[0]
	var local v2.LocalBlob
	r.NoError(v2.Scheme.Convert(res.Access, &local))
	r.Equal("application/x-tgz", local.MediaType)
	r.Equal(processed.Digest, res.Digest)
	content, _, err := target.GetLocalResource(ctx, name, version, res.ToIdentity())
	r.NoError(err)
	data := assertArchive(t, content, "first\n")
	r.Equal(godigest.FromBytes(data).Encoded(), res.Digest.Value)
	original, err := source.GetComponentVersion(ctx, name, version)
	r.NoError(err)
	r.Equal("Git/v1", original.Component.Resources[0].Access.GetType().String())
}
