package download

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"hash/crc64"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/require"
)

func TestVerifiedCopy(t *testing.T) {
	content := []byte("123456789")
	sha := sha256.Sum256(content)
	sha256Of := base64.StdEncoding.EncodeToString(sha[:])
	crc := make([]byte, 8)
	binary.BigEndian.PutUint64(crc, crc64.Checksum(content, crc64NVME))
	crc64Of := base64.StdEncoding.EncodeToString(crc)
	whole := byteRange{start: 0, end: 8, total: 9}
	part := byteRange{start: 0, end: 8, total: 20}

	tests := []struct {
		name    string
		out     *s3.GetObjectOutput
		span    byteRange
		wantErr string
	}{
		{name: "whole object with matching SHA-256", out: &s3.GetObjectOutput{ChecksumSHA256: &sha256Of}, span: whole},
		{name: "whole object with matching CRC64NVME", out: &s3.GetObjectOutput{ChecksumCRC64NVME: &crc64Of}, span: whole},
		{name: "whole object with mismatching checksum", out: &s3.GetObjectOutput{ChecksumCRC64NVME: new(base64.StdEncoding.EncodeToString(make([]byte, 8)))}, span: whole, wantErr: "fail their stored CRC64NVME checksum"},
		{name: "part with matching composite part checksum", out: &s3.GetObjectOutput{ChecksumSHA256: &sha256Of, ChecksumType: types.ChecksumTypeComposite}, span: part},
		{name: "part with mismatching composite part checksum", out: &s3.GetObjectOutput{ChecksumSHA256: new(base64.StdEncoding.EncodeToString(make([]byte, 32))), ChecksumType: types.ChecksumTypeComposite}, span: part, wantErr: "fail their stored SHA256 checksum"},
		// A full-object checksum on a partial response describes other bytes.
		{name: "part with a full-object checksum is not checked", out: &s3.GetObjectOutput{ChecksumCRC64NVME: new(base64.StdEncoding.EncodeToString(make([]byte, 8))), ChecksumType: types.ChecksumTypeFullObject}, span: part},
		{name: "composite object checksum is not checked", out: &s3.GetObjectOutput{ChecksumSHA256: new(sha256Of + "-3")}, span: whole},
		{name: "no checksum", out: &s3.GetObjectOutput{}, span: whole},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			var buf bytes.Buffer
			n, err := verifiedCopy(&buf, bytes.NewReader(content), tt.out, tt.span)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(int64(len(content)), n)
			r.Equal(content, buf.Bytes())
		})
	}
}
