/*
Copyright The ORAS Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package retry provides an HTTP transport with retry logic, adapted from
// oras.land/oras-go/v2/registry/remote/retry to remove the oras-go module
// dependency from bindings/go/http.
package retry

import (
	"errors"
	"hash/maphash"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"time"
)

const headerRetryAfter = "Retry-After"

// DefaultGenericPolicy holds the default retry parameters.
// It uses an exponential backoff with jitter and is the concrete value
// used as the fallback when no policy is configured. Callers that need
// to inherit individual fields can read from it directly.
var DefaultGenericPolicy = GenericPolicy{
	Retryable: DefaultPredicate,
	Backoff:   DefaultBackoff,
	MinWait:   200 * time.Millisecond,
	MaxWait:   3 * time.Second,
	MaxRetry:  5,
}

// DefaultPredicate retries on 5xx errors, 429, 408, and network dial timeouts.
var DefaultPredicate Predicate = func(resp *http.Response, err error) (bool, error) {
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return true, nil
		}
		return false, err
	}
	if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests {
		return true, nil
	}
	if resp.StatusCode == 0 || resp.StatusCode >= 500 {
		return true, nil
	}
	return false, nil
}

// DefaultBackoff uses exponential backoff with 10% jitter, base 250ms, factor 2.
var DefaultBackoff Backoff = ExponentialBackoff(250*time.Millisecond, 2, 0.1)

// Policy is a retry policy.
type Policy interface {
	Retry(attempt int, resp *http.Response, err error) (time.Duration, error)
}

// Predicate returns true if the request should be retried.
type Predicate func(resp *http.Response, err error) (bool, error)

// Backoff returns the duration to wait before the next retry attempt.
type Backoff func(attempt int, resp *http.Response) time.Duration

// ExponentialBackoff returns a Backoff using exponential backoff with jitter:
//
//	temp = backoff * factor^attempt
//	interval = temp*(1-jitter) + rand[0, 2*jitter*temp]
//
// On 429 responses, the Retry-After header value is used if present.
func ExponentialBackoff(backoff time.Duration, factor, jitter float64) Backoff {
	return func(attempt int, resp *http.Response) time.Duration {
		var h maphash.Hash
		h.SetSeed(maphash.MakeSeed())
		r := rand.New(rand.NewPCG(0, h.Sum64())) //nolint:gosec // non-crypto jitter is intentional

		if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			if v := resp.Header.Get(headerRetryAfter); v != "" {
				if retryAfter, _ := strconv.ParseInt(v, 10, 64); retryAfter > 0 {
					return time.Duration(retryAfter) * time.Second
				}
			}
		}

		temp := float64(backoff) * math.Pow(factor, float64(attempt))
		return time.Duration(temp*(1-jitter)) + time.Duration(r.Int64N(int64(2*jitter*temp)))
	}
}

// NewGenericPolicy returns a *GenericPolicy with DefaultPredicate and
// DefaultBackoff and the supplied retry bounds.
func NewGenericPolicy(maxRetries int, minWait, maxWait time.Duration) *GenericPolicy {
	return &GenericPolicy{
		Retryable: DefaultPredicate,
		Backoff:   DefaultBackoff,
		MinWait:   minWait,
		MaxWait:   maxWait,
		MaxRetry:  maxRetries,
	}
}

// GenericPolicy is a configurable retry policy.
type GenericPolicy struct {
	Retryable Predicate
	Backoff   Backoff
	MinWait   time.Duration
	MaxWait   time.Duration
	MaxRetry  int
}

// Retry returns the wait duration before the next attempt, or -1 to stop.
// MaxRetry semantics: 0 = infinite retries, -1 = disable (no retries), positive = max attempts.
func (p *GenericPolicy) Retry(attempt int, resp *http.Response, err error) (time.Duration, error) {
	if p.MaxRetry == -1 || (p.MaxRetry > 0 && attempt >= p.MaxRetry) {
		return -1, nil
	}
	ok, err := p.Retryable(resp, err)
	if err != nil {
		return -1, err
	}
	if !ok {
		return -1, nil
	}
	backoff := p.Backoff(attempt, resp)
	if backoff < p.MinWait {
		backoff = p.MinWait
	}
	if backoff > p.MaxWait {
		backoff = p.MaxWait
	}
	return backoff, nil
}

// Transport is an HTTP transport with retry policy.
type Transport struct {
	// Base is the underlying HTTP transport. If nil, http.DefaultTransport is used.
	Base http.RoundTripper

	// Policy returns the retry Policy for a request. If nil, DefaultGenericPolicy is used.
	Policy func() Policy
}

// DefaultClient is a client with the default retry policy.
var DefaultClient = NewClient()

// NewClient creates an HTTP client with the default retry policy.
func NewClient() *http.Client {
	return &http.Client{Transport: NewTransport(nil)}
}

// NewTransport creates an HTTP Transport with the default retry policy.
func NewTransport(base http.RoundTripper) *Transport {
	return &Transport{Base: base}
}

// RoundTrip executes the request with automatic retries per the configured Policy.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	policy := t.policy()
	attempt := 0
	for {
		resp, respErr := t.roundTrip(req)
		duration, err := policy.Retry(attempt, resp, respErr)
		if err != nil {
			if respErr == nil {
				resp.Body.Close()
			}
			return nil, err
		}
		if duration < 0 {
			return resp, respErr
		}

		if req.Body != nil {
			if req.GetBody == nil {
				return resp, respErr
			}
			body, err := req.GetBody()
			if err != nil {
				return resp, respErr
			}
			req.Body = body
		}

		if respErr == nil {
			resp.Body.Close()
		}

		timer := time.NewTimer(duration)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		attempt++
	}
}

func (t *Transport) roundTrip(req *http.Request) (*http.Response, error) {
	if t.Base == nil {
		return http.DefaultTransport.RoundTrip(req)
	}
	return t.Base.RoundTrip(req)
}

func (t *Transport) policy() Policy {
	if t.Policy == nil {
		return &DefaultGenericPolicy
	}
	return t.Policy()
}
