// Package s3 provides access to OCM resources stored as objects in an S3 or
// S3-compatible bucket.
//
// It implements the "S3" access type: a resource whose bytes are a single object
// in a bucket, described by a
// [ocm.software/open-component-model/bindings/go/s3/spec/access/v1.S3] access
// spec. Besides the bucket and object key, the spec carries optional details:
// region, media type, a pinned object version (versionId), and — for
// S3-compatible stores such as MinIO, Ceph or R2 — a custom endpoint, path-style
// addressing, and a switch to skip TLS verification. An S3 access references one
// object; it is not a component-version storage backend.
//
// [ocm.software/open-component-model/bindings/go/s3/repository.ResourceRepository]
// is the entry point. It resolves the access spec of a resource, builds an
// aws-sdk-go-v2 client, performs a GetObject, and returns the object body as a
// blob:
//
//	repo := repository.NewResourceRepository()
//	b, err := repo.DownloadResource(ctx, resource, credentials)
//	if err != nil {
//	    return err
//	}
//
// Integrity uses OCM's own SHA-256 over the content (see ProcessResourceDigest),
// not the S3 ETag, which is not a reliable whole-object hash for multipart
// objects. Upload is not yet supported (download-first, matching ocmv1).
//
// The package also provides an input method
// ([ocm.software/open-component-model/bindings/go/s3/input.InputMethod]) that, during
// component construction, downloads the object described by an
// [ocm.software/open-component-model/bindings/go/s3/spec/input/v1.S3] input spec
// (the same coordinates as the access type) and stores it as a local blob. It shares
// the download path and the S3Bucket consumer identity with the resource repository,
// so credentials configured for a bucket resolve for both.
//
// Credentials are optional. When supplied as
// [ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1.S3Credentials]
// (access key ID, secret access key, and an optional session token) they are used
// as static credentials; otherwise the AWS default credential chain applies
// (environment, shared config, and IAM instance/task roles). Legacy ocmv1 property
// names are also accepted: awsAccessKeyID, awsSecretAccessKey, and token (mapped to
// the session token).
//
// # Credential consumer identity
//
// GetResourceCredentialConsumerIdentity resolves the identity a credential resolver
// matches against. It always has the type S3Bucket and is derived from the object's
// location, so a resolver can look up matching credentials before the download:
//
//	type:     S3Bucket
//	scheme:   https                 // or the endpoint scheme (e.g. http for MinIO)
//	hostname: s3.amazonaws.com      // or the endpoint host
//	port:     9000                  // only when the endpoint sets one
//	path:     <bucketName>/<objectKey>
//
// The scheme/hostname/port are matched as a URL (an empty attribute in the credential
// config is a wildcard, and default ports are applied per scheme). The path is
// glob-matched and optional, so credentials can be scoped from broad to narrow. For a
// download of my-bucket/a/b.txt on AWS, all of these configured consumer identities
// match, most specific winning:
//
//	{type: S3Bucket, hostname: s3.amazonaws.com}                          // every bucket on the host
//	{type: S3Bucket, hostname: s3.amazonaws.com, path: my-bucket/*}       // one bucket
//	{type: S3Bucket, hostname: s3.amazonaws.com, path: my-bucket/a/*}     // a key prefix
//	{type: S3Bucket, hostname: s3.amazonaws.com, path: my-bucket/a/b.txt} // one object
//
// A config with a different type, host, explicit scheme, or a non-matching path glob
// does not resolve. region, mediaType, version and the TLS/path-style switches do not
// take part in credential matching.
//
// The wire types are each registered in their package scheme for typed
// conversion. Both the versioned (s3/v1) and unversioned (s3) type names are
// registered, and legacy upper-case access specs remain parsable because JSON
// field matching is case-insensitive.
package s3
