package ctf

import (
	"context"
	"fmt"
	"io"
	"time"
)

// newCtxReader wraps an io.Reader with one that checks ctx.Done() on each Read call.
//
// If ctx has a deadline and if r has a `SetReadDeadline(time.Time) error` method,
// then it is called with the deadline.
func newCtxReader(ctx context.Context, r io.Reader) (io.Reader, error) {
	if deadline, ok := ctx.Deadline(); ok {
		type deadliner interface {
			SetReadDeadline(time.Time) error
		}
		if d, ok := r.(deadliner); ok {
			if err := d.SetReadDeadline(deadline); err != nil {
				return nil, fmt.Errorf("failed to set read deadline: %w", err)
			}
		}
	}
	return ctxReader{ctx, r}, nil
}

//nolint:containedctx // ctxReader is an io.Reader that checks the context for cancellation so a context is important
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r ctxReader) Read(p []byte) (n int, err error) {
	if err = r.ctx.Err(); err != nil {
		return
	}
	if n, err = r.r.Read(p); err != nil {
		return
	}
	err = r.ctx.Err()
	return
}
