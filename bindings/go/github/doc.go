// Package github provides access to GitHub repositories as OCM resources and
// sources.
//
// It implements the "GitHub" access type: a resource whose bytes are a
// repository's source archive at a pinned commit, described by a
// [ocm.software/open-component-model/bindings/go/github/spec/access/v1.GitHub]
// access spec. Besides the repository URL, the spec carries a required Commit,
// an optional Ref, and an optional APIHostname for GitHub Enterprise. The
// Commit is what gets fetched; the Ref only records where it came from and
// never selects the content. Like OCI tag->digest pinning, this keeps an
// unchanged component version verifying after the branch moves on or is
// deleted.
//
// [ocm.software/open-component-model/bindings/go/github/repository/resource.ResourceRepository]
// is the entry point. It downloads the commit archive via the GitHub REST API
// as a gzipped tar blob (application/x-tgz), buffered in memory:
//
//	repo := resource.NewResourceRepository()
//	archive, err := repo.DownloadResource(ctx, res, creds)
//	if err != nil {
//	    return err
//	}
//
// The archive bytes are the exact tarball GitHub serves rather than an archive
// reconstructed locally, so the genericBlobDigest/v1 digest of a by-reference
// resource matches the digest it would carry as an embedded local blob and is
// reproducible for anyone fetching the same commit.
//
// Credentials are optional. When supplied as
// [ocm.software/open-component-model/bindings/go/github/spec/credentials/v1.GitHubCredentials]
// the token authenticates the archive download and any ref drift check; a
// token-less credential falls back to an anonymous request against GitHub's
// per-IP rate limit. The repository derives the credential consumer identity
// (type GitHubRepository) from a resource's repository URL via
// GetResourceCredentialConsumerIdentity, so a credential resolver can look up
// matching credentials before the download.
//
// Sources may carry the same access spec, but only as provenance metadata:
// component construction records it verbatim without digesting or fetching
// anything, so this module implements no source download.
// [ocm.software/open-component-model/bindings/go/github/digest.DigestProcessor]
// computes the genericBlobDigest/v1 over the downloaded archive, so
// by-reference resources carry a verifiable digest before transport.
//
// The wire types are registered in their package schemes for typed conversion.
// The canonical type is "GitHub"; the lowercase "github" and camelCase
// "gitHub" spellings used by the OCM spec remain parsable as deprecated
// aliases.
package github
