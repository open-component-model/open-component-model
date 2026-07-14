// Package source implements [repository.SourceRepository] for the GitHub/v1
// access type.
//
// The [SourceRepository] downloads a GitHub repository's source archive at a
// pinned commit via the GitHub REST API and returns it as a gzipped tar blob
// (application/x-tgz).
//
// # Usage
//
//	repo := source.NewSourceRepository()
//
//	src := &descriptor.Source{
//		Access: &v1.GitHub{
//			Type:    runtime.NewVersionedType(v1.Type, "v1"),
//			RepoURL: "https://github.com/open-component-model/ocm",
//			Commit:  "e39625d6e919582dcd25a7e6f7dd67f38a1b4f0a",
//		},
//	}
//
//	// Download the commit archive as an application/x-tgz blob.
//	archive, err := repo.DownloadSource(ctx, src)
package source
