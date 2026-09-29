package internal

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"

	gitv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	gitv1alpha1 "ocm.software/open-component-model/bindings/go/git/transformation/spec/v1alpha1"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

func gitResource(commit string) descriptor.Resource {
	return descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "my-source", Version: "1.0.0"},
		},
		Type:     "directoryTree",
		Relation: descriptor.ExternalRelation,
		Access: &gitv1.Git{
			Type:       runtime.NewVersionedType(gitv1.Type, gitv1.Version),
			Repository: "https://example.com/org/repo.git",
			Ref:        "refs/heads/main",
			Commit:     commit,
		},
	}
}

func TestProcessGit(t *testing.T) {
	for _, tt := range []struct {
		name     string
		target   runtime.Typed
		wantType runtime.Type
	}{
		{"OCI", testOCIRepo("ghcr.io/target"), ociv1alpha1.OCIAddLocalResourceV1alpha1},
		{"CTF", testCTFRepo("/tmp/target"), ociv1alpha1.CTFAddLocalResourceV1alpha1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			res := gitResource("f58349914e3c775747dc1ee9af1bc83db4652266")
			desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{res}, nil)
			v2desc, err := descriptor.ConvertToV2(scheme, desc)
			r.NoError(err)
			resource := v2desc.Component.Resources[0]
			tgd := &transformv1alpha1.TransformationGraphDefinition{}
			ids := map[int]string{0: "existing"}
			exprs, err := processResource(resource, res.Access, "root", &discoveryValue{Descriptor: desc}, tgd, tt.target, ids, 1, transferv1alpha1.UploadAsOciArtifact)
			r.NoError(err)
			r.Len(tgd.Transformations, 2)
			get, add := tgd.Transformations[0], tgd.Transformations[1]
			r.Equal(gitv1alpha1.GetGitResourceV1alpha1, get.Type)
			r.Equal(tt.wantType, add.Type)
			r.Equal("${"+get.ID+".output.file}", add.Spec.Data["file"])
			r.Equal([]string{"${" + add.ID + ".spec.file}"}, exprs)
			r.Equal(map[int]string{0: "existing", 1: add.ID}, ids)
			wantSpec, err := runtime.UnstructuredFromMixedData(map[string]any{"resource": resource})
			r.NoError(err)
			r.Equal(wantSpec.Data, get.Spec.Data)
			addedResource := add.Spec.Data["resource"].(map[string]any)
			r.Equal("${"+get.ID+".output.resource.name}", addedResource["name"])
			r.Equal("my-source", addedResource["access"].(map[string]any)["referenceName"])
		})
	}
}

func TestProcessGit_RejectsUnpinnedAccess(t *testing.T) {
	r := require.New(t)
	res := gitResource("")
	desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{res}, nil)
	v2desc, err := descriptor.ConvertToV2(scheme, desc)
	r.NoError(err)
	tgd := &transformv1alpha1.TransformationGraphDefinition{}
	ids := map[int]string{}
	exprs, err := processResource(v2desc.Component.Resources[0], res.Access, "root", &discoveryValue{Descriptor: desc}, tgd, testOCIRepo("ghcr.io/target"), ids, 0, "")
	r.ErrorContains(err, "cannot process Git resource")
	r.ErrorContains(err, "no pinned commit")
	r.ErrorContains(err, "refs/heads/main")
	r.Empty(tgd.Transformations)
	r.Empty(ids)
	r.Empty(exprs)
}

func TestProcessGit_UnsupportedTargetDoesNotEmitNodes(t *testing.T) {
	r := require.New(t)
	res := gitResource("f58349914e3c775747dc1ee9af1bc83db4652266")
	desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{res}, nil)
	v2desc, err := descriptor.ConvertToV2(scheme, desc)
	r.NoError(err)
	tgd := &transformv1alpha1.TransformationGraphDefinition{}
	ids := map[int]string{}
	err = processGit(v2desc.Component.Resources[0], res.Access.(*gitv1.Git), "root", &discoveryValue{Descriptor: desc}, tgd, &runtime.Unstructured{}, ids, 0)
	r.ErrorContains(err, "failed to create local resource upload transformation")
	r.Empty(tgd.Transformations)
	r.Empty(ids)
}

func TestBuildGraphDefinition_GitResource(t *testing.T) {
	for _, accessType := range []runtime.Type{
		runtime.NewVersionedType("Git", "v1"),
		runtime.NewUnversionedType("Git"),
		runtime.NewUnversionedType("git"),
		runtime.NewVersionedType("git", "v1alpha1"),
		runtime.NewVersionedType("Git", "v1alpha1"),
	} {
		t.Run(accessType.String(), func(t *testing.T) {
			r := require.New(t)
			res := gitResource("f58349914e3c775747dc1ee9af1bc83db4652266")
			res.Access.(*gitv1.Git).Type = accessType
			desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{res}, nil)
			resolver := testResolverFor("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/source"), desc)
			roots := testTransferRoots("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/target"), resolver)
			tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{
				CopyMode:   transferv1alpha1.CopyModeAllResources,
				UploadType: transferv1alpha1.UploadAsOciArtifact,
			}, nil)
			r.NoError(err)
			r.Len(tgd.Transformations, 4)
			r.Equal(gitv1alpha1.GetGitResourceV1alpha1, tgd.Transformations[0].Type)
			r.Equal(ociv1alpha1.OCIAddLocalResourceV1alpha1, tgd.Transformations[1].Type)
			r.Equal(FileCleanupVersionedType, tgd.Transformations[3].Type)
			r.Equal([]any{"${" + tgd.Transformations[1].ID + ".spec.file}"}, tgd.Transformations[3].Spec.Data["files"])
			_, err = NewDefaultBuilder(nil, nil, nil, nil).BuildAndCheck(tgd)
			r.NoError(err, "default builder must resolve GetGitResource and its output references")
		})
	}
}

func TestBuildGraphDefinition_GitResource_LocalCopySkipsUnpinned(t *testing.T) {
	r := require.New(t)
	desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{gitResource("")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/source"), desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/target"), resolver)
	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{
		CopyMode: transferv1alpha1.CopyModeLocalBlobResources,
	}, nil)
	r.NoError(err)
	r.Len(tgd.Transformations, 1)
	r.Equal(ociv1alpha1.OCIAddComponentVersionV1alpha1, tgd.Transformations[0].Type)
	_, err = NewDefaultBuilder(nil, nil, nil, nil).BuildAndCheck(tgd)
	r.NoError(err)
}
