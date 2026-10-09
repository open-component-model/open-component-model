package signing

import (
	"context"
	"time"
)

type trustedSigningTimeKey struct{}

// WithTrustedSigningTime returns a context carrying a signing time that the
// caller has established through a trusted source, such as an RFC 3161 timestamp
// validated against trusted TSA roots. Verifiers may validate signing
// certificates as of this time instead of the current time.
//
// Context values do not cross plugin process boundaries, so only in-process
// handlers can honour it.
func WithTrustedSigningTime(ctx context.Context, t time.Time) context.Context {
	return context.WithValue(ctx, trustedSigningTimeKey{}, t)
}

// TrustedSigningTimeFrom returns the signing time set by WithTrustedSigningTime.
func TrustedSigningTimeFrom(ctx context.Context) (time.Time, bool) {
	t, ok := ctx.Value(trustedSigningTimeKey{}).(time.Time)
	return t, ok
}
