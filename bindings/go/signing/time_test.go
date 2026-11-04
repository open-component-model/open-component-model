package signing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTrustedSigningTime(t *testing.T) {
	r := require.New(t)

	_, ok := TrustedSigningTimeFrom(t.Context())
	r.False(ok)

	signedAt := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	got, ok := TrustedSigningTimeFrom(WithTrustedSigningTime(t.Context(), signedAt))
	r.True(ok)
	r.Equal(signedAt, got)
}
