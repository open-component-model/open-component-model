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
// The wire types are each registered in their package scheme for typed
// conversion. Both the versioned (s3/v1) and unversioned (s3) type names are
// registered, and legacy upper-case access specs remain parsable because JSON
// field matching is case-insensitive.
package s3
