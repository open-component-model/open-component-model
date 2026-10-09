package internal

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	gitv1alpha1 "ocm.software/open-component-model/bindings/go/git/transformation/spec/v1alpha1"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

func TestBuildGraphDefinition_GitUploader(t *testing.T) {
	const commit = "f58349914e3c775747dc1ee9af1bc83db4652266"

	withGitArchive := localBlobResource("my-source", "1.0.0")
	withGitArchive.Access.(*descriptorv2.LocalBlob).MediaType = transferv1alpha1.GitLocalBlobMediaType

	// withoutOrigin is a local blob as a Git constructor input creates it.
	withoutOrigin := localBlobResource("my-source", "1.0.0")
	withoutOrigin.Access.(*descriptorv2.LocalBlob).MediaType = gitArchiveMediaType

	getAdd := func(get runtime.Type) []runtime.Type {
		return []runtime.Type{get, gitv1alpha1.AddGitResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType}
	}

	tests := []struct {
		name string
		// target defaults to an OCI registry.
		target     runtime.Typed
		resource   descriptor.Resource
		uploader   *transferv1alpha1.GitUploaderConfig
		wantTypes  []runtime.Type
		wantAccess map[string]string
		wantErr    string
	}{
		{
			name:      "Git access is pushed to the configured repository and ref",
			resource:  gitResource(commit),
			uploader:  &transferv1alpha1.GitUploaderConfig{Repository: "https://git.target.example/mirror/repo.git", Ref: "refs/heads/main"},
			wantTypes: getAdd(gitv1alpha1.GetGitResourceV1alpha1),
			wantAccess: map[string]string{
				"repository": "https://git.target.example/mirror/repo.git",
				"ref":        "refs/heads/main",
				"commit":     commit,
			},
		},
		{
			name:     "a Git archive local blob is pushed with explicit match, repository and ref",
			resource: withGitArchive,
			uploader: &transferv1alpha1.GitUploaderConfig{
				Match:      `resource.access.isType("LocalBlob") && resource.access.mediaType == "` + transferv1alpha1.GitLocalBlobMediaType + `"`,
				Repository: "https://git.target.example/mirror/repo.git",
				Ref:        "refs/heads/main",
			},
			wantTypes: getAdd(ociv1alpha1.OCIGetLocalResourceV1alpha1),
			wantAccess: map[string]string{
				"repository": "https://git.target.example/mirror/repo.git",
				"ref":        "refs/heads/main",
			},
		},
		{
			name:     "a local blob without origin is pushed with explicit match, repository and ref",
			resource: withoutOrigin,
			uploader: &transferv1alpha1.GitUploaderConfig{
				Match:      `resource.access.isType("LocalBlob")`,
				Repository: "/srv/git/repo.git",
				Ref:        "refs/heads/release",
			},
			wantTypes: getAdd(ociv1alpha1.OCIGetLocalResourceV1alpha1),
			wantAccess: map[string]string{
				"repository": "/srv/git/repo.git",
				"ref":        "refs/heads/release",
			},
		},
		{
			name:     "templates see resource, component, target and toGit()",
			resource: gitResource(commit),
			uploader: &transferv1alpha1.GitUploaderConfig{
				Repository: `${"https://git.target.example/" + component.version + "/" + resource.access.toGit().repository}`,
				Ref:        `${"refs/tags/" + component.version + "-" + target.type}`,
			},
			wantTypes: getAdd(gitv1alpha1.GetGitResourceV1alpha1),
			wantAccess: map[string]string{
				"repository": "https://git.target.example/1.0.0/org/repo.git",
				"ref":        "refs/tags/1.0.0-OCIRepository",
				"commit":     commit,
			},
		},
		{
			name:      "the default match pushes regardless of a CTF target",
			target:    testCTFRepo("/tmp/target"),
			resource:  gitResource(commit),
			uploader:  &transferv1alpha1.GitUploaderConfig{Repository: "https://git.target.example/org/repo.git", Ref: "refs/heads/main"},
			wantTypes: []runtime.Type{
				gitv1alpha1.GetGitResourceV1alpha1, gitv1alpha1.AddGitResourceV1alpha1,
				ociv1alpha1.CTFAddComponentVersionV1alpha1, FileCleanupVersionedType,
			},
			wantAccess: map[string]string{
				"repository": "https://git.target.example/org/repo.git",
				"ref":        "refs/heads/main",
				"commit":     commit,
			},
		},
		{
			name:      "the default match does not select other access types",
			resource:  wgetResource("blob", "1.0.0", "https://source.example/blob.tar"),
			uploader:  &transferv1alpha1.GitUploaderConfig{Repository: "https://git.target.example/repo.git", Ref: "refs/heads/main"},
			wantTypes: []runtime.Type{ociv1alpha1.OCIAddComponentVersionV1alpha1},
		},
		{
			name:     "a Git access without a pinned commit fails the build",
			resource: gitResource(""),
			uploader: &transferv1alpha1.GitUploaderConfig{Repository: "https://git.target.example/repo.git", Ref: "refs/heads/main"},
			wantErr:  "no pinned commit",
		},
		{
			name:     "a configured short ref fails the build",
			resource: gitResource(commit),
			uploader: &transferv1alpha1.GitUploaderConfig{Repository: "https://git.target.example/repo.git", Ref: "main"},
			wantErr:  `ref "main" is a short name, which does not say whether it is a branch or a tag; set ref in the git uploader config`,
		},
		{
			name:     "a Git access without a configured ref fails the build",
			resource: gitResource(commit),
			uploader: &transferv1alpha1.GitUploaderConfig{Repository: "https://git.target.example/repo.git"},
			wantErr:  `ref "" is not a full branch or tag ref`,
		},
		{
			name:     "a Git archive local blob needs repository and ref",
			resource: withGitArchive,
			uploader: &transferv1alpha1.GitUploaderConfig{Match: `resource.access.isType("LocalBlob")`, Repository: "https://git.target.example/repo.git"},
			wantErr:  "set repository and ref in the git uploader config",
		},
		{
			name:      "the default match does not select a Git archive local blob",
			resource:  withGitArchive,
			uploader:  &transferv1alpha1.GitUploaderConfig{Repository: "https://git.target.example/repo.git", Ref: "refs/heads/main"},
			wantTypes: []runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType},
		},
		{
			name:      "the default match does not select a local blob without origin",
			resource:  withoutOrigin,
			uploader:  &transferv1alpha1.GitUploaderConfig{Repository: "https://git.target.example/repo.git", Ref: "refs/heads/main"},
			wantTypes: []runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType},
		},
		{
			name:     "a local blob that is not a Git archive fails the build",
			resource: localBlobResource("my-resource", "1.0.0"),
			uploader: &transferv1alpha1.GitUploaderConfig{Match: `resource.access.isType("LocalBlob")`, Repository: "https://git.target.example/repo.git", Ref: "refs/heads/main"},
			wantErr:  "not a Git archive",
		},
		{
			name:     "a selected access type the Git uploader cannot upload fails the build",
			resource: wgetResource("blob", "1.0.0", "https://source.example/blob.tar"),
			uploader: &transferv1alpha1.GitUploaderConfig{Match: `resource.access.isType("Wget")`, Repository: "https://git.target.example/repo.git", Ref: "refs/heads/main"},
			wantErr:  "git uploader cannot upload access type",
		},
		{
			name:     "a repository template that does not evaluate fails the build",
			resource: gitResource(commit),
			uploader: &transferv1alpha1.GitUploaderConfig{Repository: `${target.filePath}`, Ref: "refs/heads/main"},
			wantErr:  "repository does not evaluate",
		},
		{
			name:     "an invalid repository fails the build",
			resource: gitResource(commit),
			uploader: &transferv1alpha1.GitUploaderConfig{Repository: "https://", Ref: "refs/heads/main"},
			wantErr:  "invalid git upload target",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{tc.resource}, nil)
			resolver := testResolverFor("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/source"), desc)
			target := tc.target
			if target == nil {
				target = testOCIRepo("ghcr.io/target")
			}
			roots := testTransferRoots("ocm.software/test", "1.0.0", target, resolver)
			tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, []transferv1alpha1.UploaderConfig{tc.uploader})
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.wantTypes, transformationTypes(tgd))
			_, err = NewDefaultBuilder(nil, nil, nil, nil).BuildAndCheck(tgd)
			r.NoError(err, "default builder must resolve AddGitResource and its references")
			if tc.wantAccess == nil {
				return
			}

			add := tgd.Transformations[1]
			r.Equal([]any{"${" + add.ID + ".spec.file}"}, tgd.Transformations[3].Spec.Data["files"])
			access := add.Spec.Data["resource"].(map[string]any)["access"].(map[string]any)
			r.Equal("Git/v1", access["type"])
			got := map[string]string{}
			for _, field := range []string{"repository", "ref", "commit"} {
				if value, ok := access[field].(string); ok {
					got[field] = evaluateTemplate(t, tgd, value)
				}
			}
			r.Equal(tc.wantAccess, got)
		})
	}
}
