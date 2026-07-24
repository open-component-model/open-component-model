// Package github provides access to GitHub repositories as OCM resources and
// sources.
//
// It implements the "GitHub" access type: a resource whose bytes are a
// repository's source archive at a pinned commit, described by a
// [ocm.software/open-component-model/bindings/go/github/spec/access/v1.GitHub]
// access spec. Besides the repository URL, the spec carries a Commit, a Ref,
// and an optional APIHostname for GitHub Enterprise. A set Commit is
// authoritative and a Ref is informational: once a commit is present it is
// never re-resolved, mirroring OCI tag->digest pinning, so a component version
// that has not changed keeps verifying even after the branch moves past the
// commit or is deleted.
//
// [ocm.software/open-component-model/bindings/go/github/repository/resource.ResourceRepository]
// is the entry point. It resolves a ref-only access to the commit the ref
// currently points at and downloads the commit archive via the GitHub REST API
// as a gzipped tar blob (application/x-tgz) that streams directly from GitHub,
// computing its digest on the fly and verifying a resource-supplied digest as
// the stream is consumed:
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
// the token authenticates both the ref resolution and the archive download; a
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
// pins a ref-only access to the commit its ref resolves to and computes the
// genericBlobDigest/v1 over the downloaded archive, so by-reference resources
// carry a verifiable digest before transport.
//
// The wire types are registered in their package schemes for typed conversion.
// The canonical type is "GitHub"; the lowercase "github" and camelCase
// "gitHub" spellings used by the OCM spec remain parsable as deprecated
// aliases.
package github
