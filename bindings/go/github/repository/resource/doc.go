// Package resource implements [repository.ResourceRepository] for the GitHub/v1
// access type.
//
// The [ResourceRepository] downloads a GitHub repository's source archive at a
// pinned commit via the GitHub REST API and returns it as a gzipped tar blob
// (application/x-tgz). A resource may be pinned to a commit or carry only a ref,
// which [ResourceRepository.ResolveCommit] resolves to the commit it currently
// points at; when both are set the commit wins.
//
// # Usage
//
//	repo := resource.NewResourceRepository()
//
//	res := &descriptor.Resource{
//		Access: &v1.GitHub{
//			Type:    runtime.NewVersionedType(v1.Type, "v1"),
//			RepoURL: "https://github.com/open-component-model/ocm",
//			Ref:     "refs/heads/main",
//		},
//	}
//
//	// Resolve the credential consumer identity, then the credentials for it.
//	identity, err := repo.GetResourceCredentialConsumerIdentity(ctx, res)
//	creds, err := credentialProvider.Resolve(ctx, identity)
//
//	// Download the commit archive as an application/x-tgz blob.
//	archive, err := repo.DownloadResource(ctx, res, creds)
package resource
