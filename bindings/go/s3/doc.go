// Package s3 provides access to OCM resources stored as objects in an S3 or
// S3-compatible bucket.
//
// It implements the "S3Bucket" access type. The content of such a resource is a
// single object in a bucket, described by a
// [ocm.software/open-component-model/bindings/go/s3/spec/access/v1.S3Bucket]
// access spec. Besides the bucket and the object key, the spec holds a region, a
// media type and a pinned object version (versionId). For an S3-compatible store
// such as MinIO, Ceph or R2 it also holds a custom endpoint and a path-style
// switch. The type addresses one object, not a component version storage backend,
// and it is not the ocmv1 "s3" access type; see "Wire types" below.
//
// [ocm.software/open-component-model/bindings/go/s3/repository.ResourceRepository]
// is the entry point. It resolves the access spec of a resource, builds an
// aws-sdk-go-v2 client, sends a GetObject request and returns the object body as a
// blob:
//
//	repo := repository.NewResourceRepository(filesystemConfig)
//	b, err := repo.DownloadResource(ctx, resource, credentials)
//	if err != nil {
//	    return err
//	}
//
// The package streams an object into a file under the TempFolder of the supplied
// filesystem config; a nil config selects the OS temporary directory. It does not
// hold the object in memory, so the memory use does not grow with the object size.
// That file outlives the call, and the caller owns it. Upload is not supported yet:
// the package downloads only, the same as ocmv1. Integrity comes from OCM's own
// SHA-256 over the content; see ProcessResourceDigest. The package does not use the
// S3 ETag, which is not a reliable whole-object hash for a multipart object.
//
// Credentials are optional. Supply them as
// [ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1.S3Credentials]
// (access key ID, secret access key and an optional session token), and the package
// uses them as static credentials. Supply none, and the AWS default credential chain
// applies: environment, shared config, IAM instance and task roles. The legacy ocmv1
// names awsAccessKeyID, awsSecretAccessKey and token are accepted as well.
//
// # Input method
//
// OCM can instead copy the same object into a component version while it builds the
// version. [ocm.software/open-component-model/bindings/go/s3/input.InputMethod]
// implements the resource input method of the constructor for the "S3Bucket" input
// type, described by a
// [ocm.software/open-component-model/bindings/go/s3/spec/input/v1.S3Bucket] spec that
// holds the same fields as the access spec:
//
//	resources:
//	- name: my-object
//	  type: blob
//	  input:
//	    type: S3Bucket/v1
//	    bucketName: my-bucket
//	    objectKey: path/to/blob.txt
//
// It downloads the object through the same code as a resource download, and gives it
// to the constructor as local blob data. The finished component version therefore
// holds the content and reads nothing from the bucket. The access type is different:
// it leaves the object in the bucket and reads it on every download.
//
// The input derives its credential consumer identity in the same way as the access
// type. One consumer entry therefore serves a bucket for referenced objects and for
// downloaded ones.
//
// # Object versions
//
// ProcessResourceDigest pins the access to the versionId it read the object at. If
// the spec already names a version, the request sends it as the versionId, and the
// response must return the same value; a store that reports no version is taken at
// its word. An unversioned bucket, the default on AWS, reports either no version or
// the placeholder "null". Neither of them changes after an overwrite. The package
// therefore never treats "null" as a pin, and it logs such an object rather than
// rejecting it: the signature covers the resource digest, so an overwritten object
// fails verification instead of replacing the content unnoticed. If you need
// reproducibility, enable bucket versioning, or name a versionId.
//
// # HTTP client and TLS
//
// Downloads go through the shared ocm HTTP client, built from an
// [ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1.Config]
// with timeouts, TLS settings and per-host overrides. The access spec has no switch
// to weaken TLS, so a descriptor cannot ask for a weaker connection than the
// configuration allows. A client supplied with repository.WithHTTPClient is used
// exactly as given, with its own settings.
//
// # Retries
//
// The aws-sdk-go-v2 client does the retries. It retries the whole operation, signs
// every attempt again, and classifies the S3 error codes. Transport retry is off for
// the client that the package gives to the SDK, globally and per host.
// retry.maxRetries counts the attempts after the first one; the SDK counts the first
// attempt as well:
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
// For AWS, the endpoint resolver of the SDK derives the request host. The host is not
// known when the package builds the client, so a per-host entry does not reach it and
// the global setting applies. The maximum time for a download is approximately the
// attempt count times the ocm timeout, which is 30s by default. SDK retry stops when
// it has deserialised the response headers, so it does not cover a body that fails
// during the transfer.
//
// # Credential consumer identity
//
// GetResourceCredentialConsumerIdentity resolves the identity that a credential
// resolver matches against. It always holds the type S3Bucket and the object path,
// and a host only for a custom endpoint:
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
// AWS carries no hostname because it is the default target, and the matcher requires
// equal hostnames, so a config that set one would not match. path.Match matches the
// path, and its "*" does not cross "/", so a config either omits the path or gives
// the exact bucketName/objectKey. The region, the media type, the version and the
// path-style switch take no part in the match. The legacy ocmv1 consumer identity
// (type S3, pathprefix key) does not resolve, tracked by
// https://github.com/open-component-model/ocm-project/issues/847
//
// # Wire types
//
// The package scheme registers the wire types for typed conversion. The access type
// resolves under four names. Each name is versioned or unversioned, and starts with
// an upper-case or a lower-case letter:
//
//	S3Bucket/v1
//	S3Bucket
//	s3Bucket/v1
//	s3Bucket
//
// Matching is exact: an all-lower-case s3bucket does not resolve, and the ocmv1 "s3"
// access type does not resolve. Such descriptors have to have their access type
// rewritten. Field names within the spec are matched without regard to case, as JSON
// decoding is.
//
// The input type resolves under the same four names in its own scheme, so a
// constructor spells an input exactly as a descriptor spells an access.
package s3
