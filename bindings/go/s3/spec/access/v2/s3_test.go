package v2

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestS3_UnmarshalJSON(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
		bad  bool
	}{
		{"legacy partial", `{"bucket":"new"}`, false},
		{"modern partial", `{"bucketName":"new"}`, false},
		{"null", `null`, false},
		{"malformed", `{`, true},
		{"wrong object type", `[]`, true},
		{"wrong legacy field type", `{"bucket":42}`, true},
		{"wrong modern field type", `{"bucketName":42}`, true},
		{"mixed fields", `{"bucket":"new","objectKey":"new"}`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			original := S3{BucketName: "old", ObjectKey: "key", Region: "region"}
			got := original
			err := json.Unmarshal([]byte(tt.data), &got)
			if tt.bad {
				r.Error(err)
				r.Equal(original, got)
				return
			}
			r.NoError(err)
			if tt.data != "null" {
				original.BucketName = "new"
			}
			r.Equal(original, got)
		})
	}
}

func TestS3_Validate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, (&S3{BucketName: "b", ObjectKey: "k"}).Validate())
	})
	t.Run("missing bucket", func(t *testing.T) {
		require.Error(t, (&S3{ObjectKey: "k"}).Validate())
	})
	t.Run("missing object key", func(t *testing.T) {
		require.Error(t, (&S3{BucketName: "b"}).Validate())
	})
}
