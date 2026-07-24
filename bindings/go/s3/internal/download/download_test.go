package download

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

// Test_awsStaticCredentials verifies which S3 credential shapes produce an AWS
// static credentials provider and that the session token is carried through when
// (and only when) an access key is present.
func Test_awsStaticCredentials(t *testing.T) {
	t.Run("access key and secret", func(t *testing.T) {
		p := awsStaticCredentials(&credv1.S3Credentials{AccessKeyID: "AKIA", SecretAccessKey: "secret"})
		require.NotNil(t, p)
		got, err := p.Retrieve(context.Background())
		require.NoError(t, err)
		require.Equal(t, "AKIA", got.AccessKeyID)
		require.Equal(t, "secret", got.SecretAccessKey)
		require.Empty(t, got.SessionToken)
	})

	t.Run("access key, secret and session token", func(t *testing.T) {
		p := awsStaticCredentials(&credv1.S3Credentials{AccessKeyID: "AKIA", SecretAccessKey: "secret", SessionToken: "session"})
		require.NotNil(t, p)
		got, err := p.Retrieve(context.Background())
		require.NoError(t, err)
		require.Equal(t, "AKIA", got.AccessKeyID)
		require.Equal(t, "secret", got.SecretAccessKey)
		require.Equal(t, "session", got.SessionToken)
	})

	t.Run("session token only falls back to the default chain", func(t *testing.T) {
		// A bare token is not a usable static credential; nil means the AWS default
		// credential chain is used instead.
		require.Nil(t, awsStaticCredentials(&credv1.S3Credentials{SessionToken: "session"}))
	})

	t.Run("empty and nil", func(t *testing.T) {
		require.Nil(t, awsStaticCredentials(&credv1.S3Credentials{}))
		require.Nil(t, awsStaticCredentials(nil))
	})
}
