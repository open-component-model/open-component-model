package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestS3_Validate(t *testing.T) {
	for _, tt := range []struct {
		name    string
		spec    S3
		wantErr string
	}{
		{"valid", S3{Bucket: "b", Key: "k"}, ""},
		{"missing bucket", S3{Key: "k"}, "bucket is required"},
		{"missing key", S3{Bucket: "b"}, "key is required"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			err := tt.spec.Validate()
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
			} else {
				r.NoError(err)
			}
		})
	}
}
