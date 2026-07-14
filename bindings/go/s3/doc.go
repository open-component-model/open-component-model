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
// (environment, shared config, and IAM instance/task roles). The repository
// derives the credential consumer identity from the endpoint host (or the AWS
// default host) via GetResourceCredentialConsumerIdentity, so a resolver can look
// up matching credentials before the download.
//
// The wire types are each registered in their package scheme for typed
// conversion. Both the versioned (s3/v1) and unversioned (s3) type names are
// registered, and legacy upper-case access specs remain parsable because JSON
// field matching is case-insensitive.
package s3
