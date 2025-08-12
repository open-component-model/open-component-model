package context

import "context"

type Reader interface {
	Context() context.Context
}

type Writer interface {
	SetContext(ctx context.Context)
}

type ReaderWriter interface {
	Reader
	Writer
}
