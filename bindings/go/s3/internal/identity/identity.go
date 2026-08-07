// Package identity builds the credential consumer identity shared by the S3
// resource repository and the S3 input method, so credentials configured for a
// bucket resolve for both.
package identity

import (
	"path"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
)

// awsDefaultHost is the consumer identity host used when no custom endpoint is
// configured (AWS S3).
const awsDefaultHost = "s3.amazonaws.com"

// Consumer returns the S3Bucket credential consumer identity for the object at
// endpoint (empty for AWS S3), bucketName and objectKey. Scheme, host and port come
// from the endpoint (or the AWS default host over https); bucketName/objectKey is
// encoded as the path attribute, which the default matcher treats as an optional glob
// so credentials can be scoped from host-wide down to a single object.
func Consumer(endpoint, bucketName, objectKey string) (runtime.Identity, error) {
	id, err := runtime.ParseURLToIdentity(baseURL(endpoint, bucketName, objectKey))
	if err != nil {
		return nil, err
	}
	id.SetType(runtime.NewUnversionedType(accessspec.S3BucketConsumerType))
	return id, nil
}

// baseURL builds the URL that identifies the object for credential resolution: the
// endpoint (or the AWS default host) followed by bucket/objectKey.
func baseURL(endpoint, bucketName, objectKey string) string {
	base := "https://" + awsDefaultHost
	if endpoint != "" {
		base = strings.TrimSuffix(endpoint, "/")
	}
	return base + "/" + path.Join(bucketName, objectKey)
}
