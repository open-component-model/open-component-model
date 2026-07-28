// Package s3 provides access to OCM resources stored as objects in an S3 or
// S3-compatible bucket.
//
// It implements the "S3Bucket" access type: a resource whose bytes are a single
// object in a bucket, described by a
// [ocm.software/open-component-model/bindings/go/s3/spec/access/v1.S3Bucket]
// access spec. Besides the bucket and object key, the spec carries optional
// details: region, media type, a pinned object version (versionId), and — for
// S3-compatible stores such as MinIO, Ceph or R2 — a custom endpoint, path-style
// addressing, and a switch to skip TLS verification. An S3Bucket access references
// one object; it is not a component-version storage backend.
//
// The type is named for what it addresses (a bucket) rather than for the AWS
// service, so it reads the same for the S3-compatible stores it also covers. It is
// deliberately not the ocmv1 "s3" access type and does not resolve specs written
// against that name; see "Wire types" below.
//
// [ocm.software/open-component-model/bindings/go/s3/repository.ResourceRepository]
// is the entry point. It resolves the access spec of a resource, builds an
// aws-sdk-go-v2 client, performs a GetObject, and returns the object body as a
// blob:
//
//	repo := repository.NewResourceRepository(filesystemConfig)
//	b, err := repo.DownloadResource(ctx, resource, credentials)
//	if err != nil {
//	    return err
//	}
//
// Objects are streamed into a file under the TempFolder of the supplied
// filesystem config (a nil config selects the OS temporary directory) rather
// than buffered in memory, so memory use does not scale with object size. The
// blob returned by DownloadResource reads from that file, which outlives the
// call: nothing removes it afterwards, so the caller owns it.
//
// Integrity uses OCM's own SHA-256 over the content (see ProcessResourceDigest),
// not the S3 ETag, which is not a reliable whole-object hash for multipart
// objects. Upload is not yet supported (download-first, matching ocmv1).
//
// Credentials are optional. When supplied as
// [ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1.S3Credentials]
// (access key ID, secret access key, and an optional session token) they are used
// as static credentials; otherwise the AWS default credential chain applies
// (environment, shared config, and IAM instance/task roles). Legacy ocmv1 property
// names are also accepted: awsAccessKeyID, awsSecretAccessKey, and token (mapped to
// the session token).
//
// # HTTP client and retries
//
// Downloads go through the shared ocm HTTP client, built from an
// [ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1.Config]
// (timeouts, TLS, per-host overrides, retry). The access spec's
// insecureSkipTLSVerify is folded into its TLS settings. A client supplied with
// repository.WithHTTPClient is used exactly as given and none of this applies to it.
//
// Two independent retry layers are in play, because they retry different failures.
// The transport retries a single round trip and sees only connection errors and
// status codes; that layer is what the ocm HTTP config describes. The aws-sdk-go-v2
// client retries the whole operation, re-signing every attempt, classifying S3's
// error codes (throttling, SlowDown, clock skew) and rate-limiting its own retries;
// that layer keeps the SDK defaults and is configured the AWS way, through
// AWS_MAX_ATTEMPTS and AWS_RETRY_MODE or the equivalent shared-config keys. Since
// the layers compose, the worst-case number of requests for one download is the
// product of the two attempt counts.
//
// With the defaults on both sides that product is not obvious: the transport allows
// 5 retries (6 attempts) and the SDK's standard mode allows 3, so a single failing
// download can issue up to 18 requests. The same composition applies to time: the
// ocm timeout (30s by default) bounds one transport round trip including its own
// retries, and the SDK starts a fresh one per attempt, so the wall-clock ceiling is
// roughly the SDK attempt count times that timeout, not the timeout itself. Lower
// retry.maxRetries in the ocm config, or AWS_MAX_ATTEMPTS on the SDK side, when a
// tighter bound matters; setting either to 1 attempt collapses the product to the
// other layer alone.
//
// Neither layer covers a body that fails mid-stream: SDK retry ends once the
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
// For AWS there is deliberately no hostname: AWS is the default target, so a credential
// config resolves host-agnostically and need not name the AWS host (the matcher requires
// equal hostnames, so a config that set one would not match). For a custom endpoint the
// hostname — and port, when non-default — identifies where the credentials apply and
// must be given in the config.
//
// The path is matched with path.Match, whose "*" does not cross "/", so in practice a
// config either omits the path (matching every object) or gives the exact
// bucketName/objectKey. For a download of my-bucket/a/b.txt on AWS both of these match:
//
//	{type: S3Bucket}                          // any AWS S3 object
//	{type: S3Bucket, path: my-bucket/a/b.txt} // that one object
//
// region, mediaType, version and the TLS/path-style switches do not take part in
// credential matching.
//
// The legacy ocmv1 consumer identity (type S3, pathprefix key) is not resolved: the
// flow returns a single identity, and multi-identity resolution is tracked by
// https://github.com/open-component-model/ocm-project/issues/847
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
// access type rewritten to be readable here. Field names within the spec are
// matched case-insensitively, as JSON decoding is.
package s3
