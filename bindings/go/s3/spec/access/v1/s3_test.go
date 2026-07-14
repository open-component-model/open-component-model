package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
