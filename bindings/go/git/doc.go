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
// The blob is an uncompressed tar (application/x-tar) of the commit's tree. Names,
// modes and symlink targets are read from the tree objects rather than from a
// checkout, so nothing about the host reaches the archive: entries carry uid and gid
// 0, no user or group name, a zero modification time and one of three modes, 0644,
// 0755, or 0777 on a symlink. Directories have explicit entries with mode 0755.
// Entries follow Git tree order. Submodules are unsupported and omitted entirely.
// The archive stays uncompressed because
// its digest is verified on other machines and the output of the standard library
// compressors is not stable across Go releases. Two callers archiving the same commit
// therefore produce the same bytes.
//
// WithMaxArchiveSize caps those bytes, at 1 GiB by default. Git transfers the
// repository before the archive exists, so the limit rejects an oversized archive
// rather than stopping the clone that produced it.
//
// # Fetching
//
// A ref-only access is cloned bare with all tags. A pinned commit is fetched on its
// own, which avoids transferring every ref and its history; servers that do not
// advertise allow-tip-sha1-in-want or allow-reachable-sha1-in-want reject that
// request, and the fetch of all refs is the fallback. A ref naming a branch resolves
// against refs/remotes/origin first and against the ref as given second, so "main",
// "refs/heads/main" and "HEAD" all work. An annotated tag is peeled to the commit it
// points at.
//
// # Digests
//
// ProcessResourceDigest pins a ref-only access to the commit its ref currently
// resolves to and computes the genericBlobDigest/v1 SHA-256 over the archive, in the
// same download. A digest already on the resource is verified rather than replaced,
// so re-digesting cannot quietly restate what a signature covers.
//
// OCM v1 calculates git digests differently, so its digests are not supported here.
// It packed a working tree checkout into a tar.gz and hashed the compressed bytes,
// and its tar headers carried the uid and username of whoever ran the command, so
// the same commit yielded a different digest for every user. The access spec is
// unchanged and resolves under the OCM v1 spellings, but a resource digested by one
// version has to be digested again for the other.
//
// # Credentials
//
// Credentials are optional and given as
// [ocm.software/open-component-model/bindings/go/git/spec/credentials/v1.GitCredentials].
// The first field that applies decides the method: a PrivateKeyPEM or PrivateKey
// authenticates an SSH repository, with Password read as the key passphrase; a Token
// or a Username authenticates an HTTPS repository, the latter with Password as the
// HTTP password. Tokens and passwords are refused over plain HTTP, as is a repository
// URL carrying credentials in its userinfo, so neither is sent in clear text. Without
// credentials an SSH repository falls back to the SSH agent and anything else is
// fetched anonymously.
//
// SSH host keys are verified against the known_hosts files of the current user unless
// WithHostKeyCallback replaces that. WithCABundle adds PEM certificates to the system
// TLS trust roots; for an HTTPS repository configured through WithHTTPConfig the
// bundle belongs in that config instead, since go-git requires the per-operation
// bundle and a plain transport together. Note that WithHTTPConfig installs its client
// into go-git's protocol registry, which is process global.
//
// # Credential consumer identity
//
// GetResourceCredentialConsumerIdentity resolves the identity a credential resolver
// matches against. It carries the type Git and the endpoint of the repository:
//
//	type:     Git
//	scheme:   https               // the URL protocol: https, http, ssh, git or file
//	hostname: github.com          // localhost for a local repository
//	port:     7999                // only when the URL sets one
//	path:     org/repo.git        // the repository path, without a leading slash
//
// Repository URLs may be http(s)://, ssh:// or git:// URLs, the scp-like form
// user@host:path, or a local repository as file:///path or a plain filesystem path.
//
// # Wire types
//
// The wire types are registered in their package schemes for typed conversion. The
// canonical access type is "Git"; the OCM v1 spellings remain parsable:
//
//	Git/v1
//	Git
//	git
//	git/v1alpha1
//	Git/v1alpha1
package git
