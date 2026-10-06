package download

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ObjectInfo is what a [Head] learns about an object without transferring its body.
// Each checksum is hex-encoded and covers the whole object, or is empty when the store
// reports none that does.
type ObjectInfo struct {
	SHA256 string
	SHA512 string
	// VersionID is the object version the store answered for; see [Result.VersionID].
	VersionID string
}

// Head issues a HeadObject for the object described by req, asking the store for the
// checksum it keeps alongside the object. It uses the same client, credentials and
// region correction as [Download]; the size limit and temporary directory do not apply.
func Head(ctx context.Context, req Request, opts ...Option) (*ObjectInfo, error) {
	o := &option{}
	for _, opt := range opts {
		opt(o)
	}

	if err := req.validate(); err != nil {
		return nil, err
	}

	client, err := newClient(ctx, req, o)
	if err != nil {
		return nil, err
	}

	in := &s3.HeadObjectInput{
		Bucket:       new(req.BucketName),
		Key:          new(req.ObjectKey),
		ChecksumMode: types.ChecksumModeEnabled,
	}
	if req.Version != "" {
		in.VersionId = new(req.Version)
	}

	out, _, err := inBucketRegion(ctx, client, req, func(optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
		return client.HeadObject(ctx, in, optFns...)
	})
	if err != nil {
		return nil, fmt.Errorf("error heading s3 object %s/%s: %w", req.BucketName, req.ObjectKey, err)
	}

	return &ObjectInfo{
		SHA256:    fullObjectChecksum(out.ChecksumType, aws.ToString(out.ChecksumSHA256), sha256.Size),
		SHA512:    fullObjectChecksum(out.ChecksumType, aws.ToString(out.ChecksumSHA512), sha512.Size),
		VersionID: aws.ToString(out.VersionId),
	}, nil
}

// fullObjectChecksum returns the hex form of a base64 checksum of size bytes, or empty
// unless it covers the whole object. A multipart upload reports a COMPOSITE checksum,
// the hash of its part hashes suffixed with "-<parts>", which is no digest of the
// content. Stores that omit the checksum type are judged by the value alone: a
// composite never decodes to exactly one hash.
func fullObjectChecksum(checksumType types.ChecksumType, value string, size int) string {
	if checksumType == types.ChecksumTypeComposite {
		return ""
	}
	sum, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(sum) != size {
		return ""
	}
	return hex.EncodeToString(sum)
}
