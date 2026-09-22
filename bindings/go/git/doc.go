// Package git provides access to Git repositories as OCM resources and constructor inputs.
//
// It implements the "Git" access type, described by a
// [ocm.software/open-component-model/bindings/go/git/spec/access/v1.Git]
// spec with a repository URL and a Ref, a Commit, or both. A set Commit is
// authoritative; Ref is then informational, even if the branch moves or is deleted.
//
// [ocm.software/open-component-model/bindings/go/git/repository.ResourceRepository]
// downloads the commit tree as a gzip-compressed tar (application/x-tgz):
//
//	repo := repository.NewResourceRepository(filesystemConfig)
//	b, err := repo.DownloadResource(ctx, resource, credentials)
//	if err != nil {
//	    return err
//	}
//
// The archive is streamed to TempFolder (the OS temporary directory by default).
// Its file outlives the call and belongs to the caller; temporary Git storage is
// removed. Upload is not supported. WithMaxArchiveSize caps the compressed output,
// not the preceding clone or fetch; by default it is unlimited.
//
// # Constructor input
//
// [ocm.software/open-component-model/bindings/go/git/input.InputMethod] packages a
// repository snapshot as a local blob using the same compressed archive as access.
// The [ocm.software/open-component-model/bindings/go/git/spec/input/v1.Git]
// input spec accepts repository, ref and commit; omitting both selectors uses
// remote HEAD, matching OCM v1 input behavior. Commit takes precedence over Ref.
// The constructor delegates local-blob storage and digest handling to the target storage.
//
// # Archive and digests
//
// The shared filesystem archiver reads Git objects without a host checkout.
// Entries are in lexical depth-first order, without a root entry or trailing slashes
// on directories; symlinks are kept and submodules are empty directories.
// Metadata is normalized: uid/gid 0, empty owner names, epoch modification time,
// files 0644, executables/directories 0755, and symlinks 0777. Standard-library gzip
// defaults are used; byte stability across Go releases is not guaranteed.
//
// ProcessResourceDigest pins a ref-only access and computes genericBlobDigest/v1
// SHA-256 over the compressed bytes in the same download. Existing digests are
// verified rather than silently replaced. Explicit refs work without remote HEAD;
// annotated tags are peeled to commits. Pinned commits are fetched directly, with
// an all-refs fallback when the server does not support fetching by commit hash.
//
// # Credentials
//
// Credentials are optional, supplied as
// [ocm.software/open-component-model/bindings/go/git/spec/credentials/v1.GitCredentials].
// Precedence is SSH PrivateKeyPEM or PrivateKey (Password is the passphrase), then
// HTTPS Token, then HTTPS Username/Password. Without credentials, SSH uses the
// agent and other transports fetch anonymously. Credentials on plain HTTP and
// HTTPS-to-HTTP redirects are rejected before transmission.
//
// SSH uses the current user's known_hosts unless WithHostKeyCallback overrides it.
// HTTP(S) uses the client from
// [ocm.software/open-component-model/bindings/go/git/repository.WithHTTPClient],
// which also decides TLS trust; without one, the shared OCM client defaults apply.
// Each repository hands its client to every Git operation it runs, so repositories
// with different clients are isolated within one process.
//
// # Credential consumer identity
//
// GetResourceCredentialConsumerIdentity derives the identity from the repository URL:
//
//	type:     Git
//	scheme:   https
//	hostname: github.com       // localhost for local repositories
//	port:     443              // explicit or protocol default; omitted for local paths
//	path:     org/repo.git     // without a leading slash
//
// URLs may use http(s), ssh, git or file, scp syntax (user@host:path), or local paths.
//
// # Wire types
//
// The access scheme registers Git/v1, Git, git, git/v1alpha1 and Git/v1alpha1.
// The separate input scheme registers git/v1, git, Git and Git/v1.
package git
