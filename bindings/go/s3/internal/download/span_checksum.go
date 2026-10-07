package download

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"hash"
	"hash/crc32"
	"hash/crc64"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// crc64NVME is the CRC-64/NVME table S3's CRC64NVME checksum uses, in the reflected
// form hash/crc64 expects.
var crc64NVME = crc64.MakeTable(0x9a6c9329ac4bc9b5)

// spanChecksum returns the checksum a GetObject response carries for exactly the span
// it delivers, with a hash to compare it against, or ok false when it carries none
// that does. The SDK validates checksums of complete (200) responses only, so the
// partial (206) responses of a part or range download are checked here.
//
// A checksum describes the span when the span is the whole object and the checksum is
// not a composite, or when it is the part-level checksum of a COMPOSITE object, which
// S3 returns for a part number. A FULL_OBJECT checksum on a partial span describes
// other bytes and is ignored; so are SHA-1, MD5 and XXHASH checksums, which this
// FIPS-constrained build does not compute.
func spanChecksum(out *s3.GetObjectOutput, r byteRange) (name string, h hash.Hash, want []byte, ok bool) {
	whole := r.start == 0 && r.end == r.total-1
	if !whole && out.ChecksumType != types.ChecksumTypeComposite {
		return "", nil, nil, false
	}

	for _, c := range []struct {
		name  string
		value *string
		hash  func() hash.Hash
	}{
		{"SHA256", out.ChecksumSHA256, sha256.New},
		{"SHA512", out.ChecksumSHA512, sha512.New},
		{"CRC64NVME", out.ChecksumCRC64NVME, func() hash.Hash { return crc64.New(crc64NVME) }},
		{"CRC32C", out.ChecksumCRC32C, func() hash.Hash { return crc32.New(crc32.MakeTable(crc32.Castagnoli)) }},
		{"CRC32", out.ChecksumCRC32, func() hash.Hash { return crc32.NewIEEE() }},
	} {
		value := aws.ToString(c.value)
		// A "-<parts>" suffix marks a checksum over part checksums, not over bytes.
		if value == "" || strings.Contains(value, "-") {
			continue
		}
		sum, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			continue
		}
		h := c.hash()
		if len(sum) != h.Size() {
			continue
		}
		return c.name, h, sum, true
	}
	return "", nil, nil, false
}

// verifiedCopy copies body to w and, when the response carries a checksum for span r,
// fails unless the copied bytes match it.
func verifiedCopy(w io.Writer, body io.Reader, out *s3.GetObjectOutput, r byteRange) (int64, error) {
	name, h, want, ok := spanChecksum(out, r)
	if !ok {
		return io.Copy(w, body)
	}
	n, err := io.Copy(io.MultiWriter(w, h), body)
	if err != nil {
		return n, err
	}
	if got := h.Sum(nil); !bytes.Equal(got, want) {
		return n, fmt.Errorf("bytes %d-%d fail their stored %s checksum: expected %s, got %s",
			r.start, r.end, name, base64.StdEncoding.EncodeToString(want), base64.StdEncoding.EncodeToString(got))
	}
	return n, nil
}
