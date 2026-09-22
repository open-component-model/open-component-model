// Package git provides access to Git repositories as OCM resources.
//
// It implements the "Git" access type: a resource whose bytes are a repository's
// tree at one commit, described by a
// [ocm.software/open-component-model/bindings/go/git/spec/access/v1.Git] access
// spec. The spec carries the repository URL and a Ref, a Commit, or both. A set
// Commit is authoritative and a Ref is informational: once a commit is present it
// is never re-resolved, mirroring OCI tag->digest pinning, so a component version
// that has not changed keeps verifying after the branch moves past the commit or
// is deleted.
//
// [ocm.software/open-component-model/bindings/go/git/repository.ResourceRepository]
// is the entry point. It resolves the access spec, fetches the snapshot and returns
// the archive as a blob:
//
//	repo := repository.NewResourceRepository(filesystemConfig)
//	b, err := repo.DownloadResource(ctx, resource, credentials)
//	if err != nil {
//	    return err
//	}
//
// The archive is written to a file under the TempFolder of the supplied filesystem
// config (a nil config selects the OS temporary directory) rather than buffered, so
// memory use does not scale with repository size. That file outlives the call and is
// owned by the caller. The Git objects the archive is taken from live in a directory
// of their own that the download removes again. Upload is not supported: a component
// version cannot publish a resource back into a Git repository.
//
// # Archive format
//
// The blob is a gzip-compressed tar (application/x-tgz) written by the shared
// filesystem archiver from Git objects, without a host checkout. Layout follows
// OCM v1: lexical, depth-first entries, no root entry or directory trailing slash,
// preserved symlinks, and empty submodule directories without fetching their contents.
// Metadata is normalized: uid/gid 0, empty owner names, epoch modification time,
// files 0644, executables/directories 0755, and symlinks 0777. Gzip uses the standard
// library defaults; byte stability across Go releases is not guaranteed.
//
// WithMaxArchiveSize caps compressed bytes, at 1 GiB by default. Git transfers the
// repository before the archive exists, so the limit rejects an oversized archive
// rather than stopping the clone that produced it.
//
// # Fetching
//
// HEAD is cloned bare; explicit refs are fetched without requiring a valid remote
// HEAD. A pinned commit is fetched on its own, avoiding other refs and their history;
// servers that do not
// advertise allow-tip-sha1-in-want or allow-reachable-sha1-in-want reject that
// request, and the fetch of all refs is the fallback. A ref naming a branch resolves
// against refs/remotes/origin first and against the ref as given second, so "main",
// "refs/heads/main" and "HEAD" all work. Annotated tags are peeled recursively to
// their target commit.
//
// # Digests
//
// ProcessResourceDigest pins a ref-only access to the commit its ref currently
// resolves to and computes genericBlobDigest/v1 SHA-256 over the compressed archive in the
// same download. A digest already on the resource is verified rather than replaced,
// so re-digesting cannot quietly restate what a signature covers.
//
// # OCM v1 Git access compatibility
//
// Legacy Git access spellings are accepted. Both versions hash compressed archives,
// but v1 archives with host-dependent metadata cannot be reproduced reliably here.
// Planned v1 metadata normalization (ocm-project#1337) needs separate compatibility
// validation. Existing digests are verified, never skipped or replaced. This concerns
// Git access only, not input handling.
//
// # Credentials
//
// Credentials are optional and given as
// [ocm.software/open-component-model/bindings/go/git/spec/credentials/v1.GitCredentials].
// The first field that applies decides the method: a PrivateKeyPEM or PrivateKey
// authenticates an SSH repository, with Password read as the key passphrase; a Token
// or a Username authenticates an HTTPS repository, the latter with Password as the
// HTTP password. Tokens and passwords are refused over plain HTTP, as is a repository
// URL carrying credentials in its userinfo. HTTPS-to-HTTP redirects are rejected
// before sending the redirected request. Without
// credentials an SSH repository falls back to the SSH agent and anything else is
// fetched anonymously.
//
// SSH host keys use the current user's known_hosts unless WithHostKeyCallback
// overrides verification. WithCABundle extends system TLS trust. See
// [ocm.software/open-component-model/bindings/go/git/repository.WithHTTPConfig]
// for custom HTTP configuration and its process-global transport behavior. The
// downloader installs guarded default HTTP transports at package initialization.
//
// # Credential consumer identity
//
// GetResourceCredentialConsumerIdentity resolves the identity a credential resolver
// matches against. It carries the type Git and the endpoint of the repository:
//
//	type:     Git
//	scheme:   https               // the URL protocol: https, http, ssh, git or file
//	hostname: github.com          // localhost for a local repository
//	port:     7999                // explicit or protocol default; omitted for local paths
//	path:     org/repo.git        // the repository path, without a leading slash
//
// Repository URLs may be http(s)://, ssh:// or git:// URLs, the scp-like form
// user@host:path, or a local repository as file:///path or a plain filesystem path.
//
// # Wire types
//
// The wire types are registered in their package schemes for typed conversion. The
// canonical access type is "Git/v1"; the following aliases are also accepted:
//
//	Git/v1
//	Git
//	git
//	git/v1alpha1
//	Git/v1alpha1
package git
