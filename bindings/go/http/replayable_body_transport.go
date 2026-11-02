package http

import (
	"errors"
	"io"
	nethttp "net/http"
	"reflect"
	"sync"

	"ocm.software/open-component-model/bindings/go/http/internal/retry"
)

// replayableBodyTransport sets Request.GetBody for requests whose body can be
// rewound, so the request can be resent after the first attempt consumed part
// of it: by retry.Transport, and by net/http's HTTP/2 transport itself when a
// server or load balancer retires the connection with a graceful GOAWAY.
//
// net/http only sets GetBody for *bytes.Reader, *bytes.Buffer and
// *strings.Reader. Callers such as oras-go's monolithic blob PUT pass their
// content reader straight through, so a file-backed or otherwise seekable blob
// would fail the whole upload on a routine connection recycle. Following the
// net/http convention, a body is replayable if it is seekable once io.NopCloser
// wrappers are removed; non-seekable bodies pass through unchanged.
//
// Read errors of the response body are wrapped in retry.BodyReadError, so
// IsTransientError can tell a connection cut mid-body from corrupt local data.
type replayableBodyTransport struct {
	base nethttp.RoundTripper
}

func (t *replayableBodyTransport) RoundTrip(req *nethttp.Request) (*nethttp.Response, error) {
	resp, err := t.roundTrip(req)
	if resp != nil && resp.Body != nil && resp.Body != nethttp.NoBody &&
		resp.StatusCode != nethttp.StatusSwitchingProtocols {
		resp.Body = &taggedBody{ReadCloser: resp.Body}
	}
	return resp, err
}

func (t *replayableBodyTransport) roundTrip(req *nethttp.Request) (*nethttp.Response, error) {
	if req.Body == nil || req.Body == nethttp.NoBody || req.GetBody != nil {
		return t.base.RoundTrip(req)
	}
	seeker, ok := unwrapNopClosers(req.Body).(io.ReadSeeker)
	if !ok {
		return t.base.RoundTrip(req)
	}
	start, err := seeker.Seek(0, io.SeekCurrent)
	if err != nil {
		return t.base.RoundTrip(req)
	}

	src := &rewindableBody{rs: seeker, start: start, owner: req.Body}
	replayable := req.Clone(req.Context())
	replayable.Body = src.firstAttempt()
	replayable.GetBody = src.nextAttempt
	resp, err := t.base.RoundTrip(replayable)
	src.roundTripDone(err)
	return resp, err
}

// taggedBody wraps response body read errors in retry.BodyReadError.
type taggedBody struct {
	io.ReadCloser
}

func (b *taggedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && err != io.EOF {
		err = &retry.BodyReadError{Err: err}
	}
	return n, err
}

var (
	nopCloserType         = reflect.TypeOf(io.NopCloser(nil))
	nopCloserWriterToType = reflect.TypeOf(io.NopCloser(struct {
		io.Reader
		io.WriterTo
	}{}))
)

// unwrapNopClosers strips any number of io.NopCloser layers. io.NopCloser's
// types are unexported, so reflection is the only way to see through them;
// net/http (transfer.go unwrapNopCloser) and oras-go use the same technique.
func unwrapNopClosers(r io.Reader) io.Reader {
	for {
		switch reflect.TypeOf(r) {
		case nopCloserType, nopCloserWriterToType:
			r = reflect.ValueOf(r).Field(0).Interface().(io.Reader)
		default:
			return r
		}
	}
}

var errStaleBody = errors.New("request body was superseded by a replay")

// rewindableBody hands out one body per attempt over a shared seekable source.
// Only the latest attempt may read: a failed attempt whose write loop is still
// draining must not race the replay's Seek. On success the original body is
// closed once the transport closed the final attempt, preserving the
// RoundTripper contract that the transport closes req.Body. On failure it is
// closed when the round trip returns: a retrying base may have fetched a next
// attempt via GetBody and then given up (context canceled, retries exhausted)
// without ever handing it to a transport that would close it.
type rewindableBody struct {
	mu    sync.Mutex
	rs    io.ReadSeeker
	start int64
	owner io.Closer

	generation int
	latestDone bool // the latest attempt's body was closed by the transport
	returned   bool // the outer RoundTrip returned; no further attempts
	released   bool
	closeErr   error
}

func (b *rewindableBody) firstAttempt() io.ReadCloser {
	return &bodyAttempt{src: b, generation: 0}
}

func (b *rewindableBody) nextAttempt() (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.released {
		return nil, errStaleBody
	}
	if _, err := b.rs.Seek(b.start, io.SeekStart); err != nil {
		return nil, err
	}
	b.generation++
	b.latestDone = false
	return &bodyAttempt{src: b, generation: b.generation}, nil
}

func (b *rewindableBody) roundTripDone(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.returned = true
	if err != nil {
		// Any attempt still draining sees released and stops reading.
		b.latestDone = true
	}
	b.releaseLocked()
}

func (b *rewindableBody) releaseLocked() {
	if b.released || !b.returned || !b.latestDone {
		return
	}
	b.released = true
	b.closeErr = b.owner.Close()
}

type bodyAttempt struct {
	src        *rewindableBody
	generation int
}

func (a *bodyAttempt) Read(p []byte) (int, error) {
	a.src.mu.Lock()
	defer a.src.mu.Unlock()
	if a.generation != a.src.generation || a.src.released {
		return 0, errStaleBody
	}
	return a.src.rs.Read(p)
}

func (a *bodyAttempt) Close() error {
	a.src.mu.Lock()
	defer a.src.mu.Unlock()
	if a.generation == a.src.generation {
		a.src.latestDone = true
		a.src.releaseLocked()
		return a.src.closeErr
	}
	return nil
}
