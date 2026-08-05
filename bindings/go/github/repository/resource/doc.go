// Package resource implements [repository.ResourceRepository] for the GitHub/v1
// access type.
//
// The [ResourceRepository] downloads a GitHub repository's source archive at a
// commit via the GitHub REST API and returns it as a gzipped tar blob
// (application/x-tgz). The access must name the commit. A ref, if set, is
// resolved by [ResourceRepository.ResolveCommit] only to warn when it has moved
// away from that commit.
//
// # Usage
//
//	// Archives are buffered in memory; pass WithHTTPConfig or WithHTTPClient
//	// to tune the HTTP client.
//	repo := resource.NewResourceRepository()
//
//	res := &descriptor.Resource{
//		Access: &v1.GitHub{
//			Type:    runtime.NewVersionedType(v1.Type, "v1"),
//			RepoURL: "https://github.com/open-component-model/ocm",
//			Commit:  "0123456789abcdef0123456789abcdef01234567", // required
//			Ref:     "refs/heads/main", // optional, informational
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
