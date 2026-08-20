// Package s3 provides access to OCM resources stored as objects in an S3 or
// S3-compatible bucket.
//
// It implements the "S3Bucket" access type: a resource whose bytes are a single
// object in a bucket, described by a
// [ocm.software/open-component-model/bindings/go/s3/spec/access/v1.S3Bucket]
// access spec. Besides the bucket and object key, the spec carries optional details:
// region, media type, a pinned object version (versionId), and — for S3-compatible
// stores such as MinIO, Ceph or R2 — a custom endpoint and path-style addressing. It
// carries nothing that weakens transport security; see "HTTP client and TLS" below.
// It references one object; it is not a
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
//
// Objects are streamed into a file under the TempFolder of the supplied filesystem
// config (a nil config selects the OS temporary directory) rather than buffered, so
// memory use does not scale with object size. That file outlives the call and is
// owned by the caller, who decides when it is removed.
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
// # HTTP client and TLS
//
// Downloads go through the shared ocm HTTP client, built from an
// [ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1.Config]
// (timeouts, TLS, per-host overrides). A client supplied with
// repository.WithHTTPClient is used exactly as given and none of this applies to it.
//
// TLS verification is therefore governed by one of those two and nothing else: the
// access spec has no switch to weaken it, so a descriptor cannot ask for a laxer
// connection than the configuration allows, and a client supplied with
// repository.WithHTTPClient brings its own TLS settings.
//
// # Retries
//
// Retrying is done by the aws-sdk-go-v2 client alone. It retries the whole operation,
// re-signing every attempt and classifying S3's error codes (throttling, SlowDown,
// clock skew), which a transport retrying a single round trip cannot do, it sees only
// connection errors and status codes. Retrying in both places would multiply the two
// attempt counts, so transport retry is switched off for the client handed to the SDK,
// globally and per host.
//
// The ocm retry configuration drives that one layer, so a single setting means a single
// thing. retry.maxRetries counts the attempts after the first, the SDK counts the first
// as well:
//
//	maxRetries: 3    // 4 attempts
//	maxRetries: -1   // 1 attempt, no retrying
//	maxRetries: 0    // infinite, bounded only by ctx and the SDK's retry quota
//	(unset)          // the SDK's own default of 3 attempts, honouring
//	                 // AWS_MAX_ATTEMPTS and AWS_RETRY_MODE
//
// A per-host entry for the endpoint overrides the global setting, merged over it field
// by field and matched on host:port before the bare hostname — the same policy, and the
// same matching, the ocm HTTP client would have applied to its own transport. So this
// configuration gives a MinIO download 11 attempts and every other download 4:
//
//	retry:
//	  maxRetries: 3
//	hosts:
//	  "minio.internal:9000":
//	    retry:
//	      maxRetries: 10
//
// On AWS there is no endpoint to match against: the request host is derived by the SDK's
// endpoint resolver from bucket, region, addressing style and partition, and is not known
// when the client is built. A per-host entry therefore does not reach the SDK there and
// the global setting stands.
//
// The wall-clock ceiling for a download is roughly the attempt
// count times the ocm timeout (30s by default), not the timeout itself.
//
// Retrying does not cover a body that fails mid-stream: SDK retry ends once the
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
// region, mediaType, version and the path-style switch do not take part in
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
