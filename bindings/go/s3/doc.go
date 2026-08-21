// Package s3 provides access to OCM resources stored as objects in an S3 or
// S3-compatible bucket.
//
// It implements the "S3Bucket" access type: a resource whose bytes are a single
// object in a bucket, described by a
// [ocm.software/open-component-model/bindings/go/s3/spec/access/v1.S3Bucket]
// access spec. Besides the bucket and object key, the spec carries a region, a media
// type, a pinned object version (versionId) and — for S3-compatible stores such as
// MinIO, Ceph or R2 — a custom endpoint and path-style addressing. It addresses one
// object rather than a component-version storage backend, and is deliberately not the
// ocmv1 "s3" access type; see "Wire types" below.
//
// [ocm.software/open-component-model/bindings/go/s3/repository.ResourceRepository]
// is the entry point. It resolves the access spec of a resource, builds an
// aws-sdk-go-v2 client, performs a GetObject and returns the object body as a blob:
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
// owned by the caller. Upload is not supported yet (download-first, matching ocmv1),
// and integrity uses OCM's own SHA-256 over the content (see ProcessResourceDigest)
// rather than the S3 ETag, which is not a reliable whole-object hash for multipart
// objects.
//
// Credentials are optional. When supplied as
// [ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1.S3Credentials]
// (access key ID, secret access key and an optional session token) they are used as
// static credentials; otherwise the AWS default credential chain applies
// (environment, shared config, IAM instance/task roles). The legacy ocmv1 names
// awsAccessKeyID, awsSecretAccessKey and token are accepted as well.
//
// # Object versions
//
// ProcessResourceDigest pins the access to the versionId the object was read at. A
// version already named in the spec is sent as the request's versionId and must come
// back unchanged; stores reporting no version are taken at their word. An unversioned
// bucket — the default on AWS — reports either no version or the placeholder "null",
// neither of which survives an overwrite, so "null" is never treated as a pin and
// such an object is logged rather than rejected: signing covers the resource digest,
// so an overwritten object fails verification instead of being quietly substituted.
// Enable bucket versioning, or name a versionId, where reproducibility matters.
//
// # HTTP client and TLS
//
// Downloads go through the shared ocm HTTP client, built from an
// [ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1.Config]
// (timeouts, TLS, per-host overrides). The access spec has no switch to weaken TLS,
// so a descriptor cannot ask for a laxer connection than the configuration allows. A
// client supplied with repository.WithHTTPClient is used exactly as given and brings
// its own settings.
//
// # Retries
//
// Retrying is left to the aws-sdk-go-v2 client, which retries the whole operation,
// re-signs every attempt and classifies S3's error codes; transport retry is switched
// off for the client handed to the SDK, globally and per host. retry.maxRetries counts
// the attempts after the first, the SDK counts the first as well:
//
//	maxRetries: 3    // 4 attempts
//	maxRetries: -1   // 1 attempt, no retrying
//	maxRetries: 0    // infinite, bounded only by ctx and the SDK's retry quota
//	(unset)          // the SDK's own default of 3 attempts, honouring
//	                 // AWS_MAX_ATTEMPTS and AWS_RETRY_MODE
//
// A per-host entry for the endpoint overrides the global setting, so this gives a
// MinIO download 11 attempts and every other download 4:
//
//	retry:
//	  maxRetries: 3
//	hosts:
//	  "minio.internal:9000":
//	    retry:
//	      maxRetries: 10
//
// On AWS the request host is derived by the SDK's endpoint resolver and is not known
// when the client is built, so a per-host entry does not reach it and the global
// setting stands. A download's wall-clock ceiling is roughly the attempt count times
// the ocm timeout (30s by default), and SDK retry ends once the response headers are
// deserialised, so it does not cover a body that fails mid-stream.
//
// # Credential consumer identity
//
// GetResourceCredentialConsumerIdentity resolves the identity a credential resolver
// matches against. It always carries the type S3Bucket and the object path, and a
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
// AWS carries no hostname because it is the default target and the matcher requires
// equal hostnames, so a config that set one would not match. The path is matched with
// path.Match, whose "*" does not cross "/", so a config either omits the path or gives
// the exact bucketName/objectKey; region, mediaType, version and the path-style switch
// take no part in matching. The legacy ocmv1 consumer identity (type S3, pathprefix
// key) is not resolved, tracked by
// https://github.com/open-component-model/ocm-project/issues/847
//
// # Wire types
//
// The wire types are registered in their package scheme for typed conversion. The
// access type resolves under four names — versioned and unversioned, each spelled with
// a leading upper- or lower-case letter:
//
//	S3Bucket/v1
//	S3Bucket
//	s3Bucket/v1
//	s3Bucket
//
// Matching is exact, so neither an all-lower-case s3bucket nor the ocmv1 "s3" access
// type resolves; such descriptors have to have their access type rewritten. Field
// names within the spec are matched case-insensitively, as JSON decoding is.
package s3
