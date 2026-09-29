package repositoryupload

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPoll(t *testing.T) {
	errTry := errors.New("try failed")
	tests := []struct {
		name      string
		doneAt    int // call that reports done; 0 never
		failAt    int // call that fails; 0 never
		cancel    bool
		wantDone  bool
		wantCalls int
		wantErr   error
	}{
		{name: "done on the third try", doneAt: 3, wantDone: true, wantCalls: 3},
		{name: "never done", wantCalls: PollAttempts},
		{name: "a failing try ends polling", failAt: 2, wantCalls: 2, wantErr: errTry},
		{name: "a cancelled context ends polling", cancel: true, wantCalls: 1, wantErr: context.Canceled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			interval := time.Microsecond
			if tc.cancel {
				cancel()
				interval = time.Hour
			}
			calls := 0
			done, err := Poll(ctx, interval, func() (bool, error) {
				calls++
				if calls == tc.failAt {
					return false, errTry
				}
				return calls == tc.doneAt, nil
			})
			r.ErrorIs(err, tc.wantErr)
			r.Equal(tc.wantDone, done)
			r.Equal(tc.wantCalls, calls)
		})
	}
}

func TestClientSend_RejectsRedirectOfUpload(t *testing.T) {
	r := require.New(t)
	var loginHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/login" {
			loginHits++
			_, _ = io.WriteString(w, "{}")
			return
		}
		_, _ = io.Copy(io.Discard, req.Body)
		http.Redirect(w, req, "/login", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	c := &Client{}
	err := c.Send(t.Context(), http.MethodPut, srv.URL+"/upload", strings.NewReader("content"), -1, nil, nil)
	r.ErrorContains(err, "returned status 302")
	r.Zero(loginHits, "the redirect of an upload must not be followed")

	var out any
	r.NoError(c.Send(t.Context(), http.MethodGet, srv.URL+"/upload", nil, -1, nil, &out), "reads still follow redirects")
	r.Equal(1, loginHits)
}
