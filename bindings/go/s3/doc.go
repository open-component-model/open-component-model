// Package s3 provides access to OCM resources stored as objects in an S3 or
// S3-compatible bucket.
//
// It implements the "S3Bucket" access type: a resource whose bytes are a single
// object in a bucket, described by a
// [ocm.software/open-component-model/bindings/go/s3/spec/access/v1.S3Bucket]
// access spec. Besides the bucket and object key, the spec carries optional details:
// region, media type, a pinned object version (versionId), and — for S3-compatible
// stores such as MinIO, Ceph or R2 — a custom endpoint, path-style addressing, and a
// switch to skip TLS verification. It references one object; it is not a
// component-version storage backend. It is named for what it addresses rather than
// for the AWS service, and is deliberately not the ocmv1 "s3" access type; see
// "Wire types" below.
//
// [ocm.software/open-component-model/bindings/go/s3/repository.ResourceRepository]
// is the entry point. It resolves the access spec of a resource, builds an
// aws-sdk-go-v2 client, performs a GetObject, and returns the object body as a blob:
//
//	repo := repository.NewResourceRepository(filesystemConfig)
//	b, err := repo.DownloadResource(ctx, resource, credentials)
//	if err != nil {
//	    return err
//	}
//	defer b.(io.Closer).Close()
//
// Objects are streamed into a file under the TempFolder of the supplied filesystem
// config (a nil config selects the OS temporary directory) rather than buffered, so
// memory use does not scale with object size. The returned blob owns that file:
// closing it removes the file, and a blob dropped without being closed has its file
// removed once it becomes unreachable, so downloads do not pile up in a long-running
// process.
//
// Integrity uses OCM's own SHA-256 over the content (see ProcessResourceDigest), not
// the S3 ETag, which is not a reliable whole-object hash for multipart objects.
// Upload is not yet supported (download-first, matching ocmv1).
//
// Credentials are optional. When supplied as
// [ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1.S3Credentials]
// (access key ID, secret access key, and an optional session token) they are used as
// static credentials; otherwise the AWS default credential chain applies
// (environment, shared config, and IAM instance/task roles). Legacy ocmv1 property
// names are also accepted: awsAccessKeyID, awsSecretAccessKey, and token (mapped to
// the session token).
//
// # Object versions and unversioned buckets
//
// ProcessResourceDigest pins the access to the versionId the object was read at. A
// version already named in the spec is sent as the request's versionId and must come
// back unchanged; a store answering with a different one did not serve the object the
// access names. Stores that report no version cannot be checked and are taken at
// their word.
//
// An unversioned bucket — the default on AWS — reports either no version or the
// placeholder "null", neither of which survives an overwrite. "null" is therefore
// never treated as a pin, whether the store reported it or an author wrote it into
// the spec, and such an object is logged rather than rejected.
//
// It is logged because an unpinned access risks availability, not integrity: signing
// covers the resource digest but excludes the access, and every path that persists
// content verifies the bytes against that digest first, so an overwritten object
// fails the operation instead of being quietly substituted. What is lost is the
// original bytes — a versioned bucket still holds them under the old versionId, an
// unversioned one does not — stranding component versions still in their
// S3-referencing form. Copies already transferred by value carry the object as a
// local blob and are unaffected, and constructing a new component version over the
// new content works normally. Enable bucket versioning, or name a versionId, where
// reproducibility matters.
//
// # HTTP client and retries
//
// Downloads go through the shared ocm HTTP client, built from an
// [ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1.Config]
// (timeouts, TLS, per-host overrides, retry). The access spec's insecureSkipTLSVerify
// is folded into its TLS settings. A client supplied with repository.WithHTTPClient
// is used exactly as given and none of this applies to it.
//
// Two retry layers compose, because they retry different failures. The transport
// retries a single round trip and sees only connection errors and status codes; that
// is the layer the ocm HTTP config describes. The aws-sdk-go-v2 client retries the
// whole operation, re-signing every attempt and classifying S3's error codes
// (throttling, SlowDown, clock skew); that layer keeps the SDK defaults and is
// configured the AWS way, through AWS_MAX_ATTEMPTS and AWS_RETRY_MODE.
//
// Since they compose, the worst case for one download is the product of both attempt
// counts — 6 × 3 = 18 requests with the defaults — and the wall-clock ceiling is
// roughly the SDK attempt count times the ocm timeout (30s by default), not the
// timeout itself. Lower retry.maxRetries or AWS_MAX_ATTEMPTS when a tighter bound
// matters. Neither layer covers a body that fails mid-stream: SDK retry ends once the
// response headers are deserialised, and the object is streamed after that.
//
// # Credential consumer identity
//
// GetResourceCredentialConsumerIdentity resolves the identity a credential resolver
// matches against. It always has the type S3Bucket and the object path, and carries a
// host only for a custom endpoint:
//
//	AWS S3 (no endpoint):
//		type: S3Bucket
//		path: <bucketName>/<objectKey>
//
//	custom endpoint (MinIO, Ceph, R2):
//		type:     S3Bucket
//		scheme:   https            // the endpoint scheme (http for a plaintext MinIO)
//		hostname: minio.internal   // the endpoint host
//		port:     9000             // when the endpoint sets one
//		path:     <bucketName>/<objectKey>
//
// AWS deliberately has no hostname: it is the default target, so a credential config
// resolves host-agnostically, and since the matcher requires equal hostnames a config
// that set one would not match. For a custom endpoint the hostname — and port, when
// non-default — identifies where the credentials apply and must be given.
//
// The path is matched with path.Match, whose "*" does not cross "/", so a config
// either omits the path or gives the exact bucketName/objectKey. For a download of
// my-bucket/a/b.txt on AWS both of these match:
//
//	{type: S3Bucket}                          // any AWS S3 object
//	{type: S3Bucket, path: my-bucket/a/b.txt} // that one object
//
// region, mediaType, version and the TLS/path-style switches do not take part in
// credential matching. The legacy ocmv1 consumer identity (type S3, pathprefix key)
// is not resolved: the flow returns a single identity, and multi-identity resolution
// is tracked by https://github.com/open-component-model/ocm-project/issues/847
//
// # Wire types
//
// The wire types are registered in their package scheme for typed conversion. The
// access type resolves under four names — versioned and unversioned, each spelled
// with a leading upper-case or lower-case letter:
//
//	S3Bucket/v1
//	S3Bucket
//	s3Bucket/v1
//	s3Bucket
//
// Matching is exact, so no other spelling resolves: neither an all-lower-case
// s3bucket nor the ocmv1 "s3" access type, whose descriptors have to have their
// access type rewritten to be readable here. Field names within the spec are matched
// case-insensitively, as JSON decoding is.
package s3
