package oci

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestResult(doe descriptorOrError) *Result {
	ch := make(chan descriptorOrError, 1)
	ch <- doe
	return &Result{desc: ch}
}

func TestDescriptor_ReturnsCachedResult(t *testing.T) {
	expected := descriptorOrError{
		Descriptor: ociImageSpecV1.Descriptor{
			MediaType: "application/vnd.test",
			Digest:    digest.FromString("test"),
			Size:      42,
		},
	}
	r := newTestResult(expected)

	ctx := t.Context()

	// First call should receive from channel and cache.
	desc1, err1 := r.Descriptor(ctx)
	require.NoError(t, err1)
	assert.Equal(t, expected.Descriptor, desc1)

	// Second call should return cached result immediately.
	desc2, err2 := r.Descriptor(ctx)
	require.NoError(t, err2)
	assert.Equal(t, expected.Descriptor, desc2)
}

func TestDescriptor_CachedErrorIsReturned(t *testing.T) {
	expected := descriptorOrError{
		Err: assert.AnError,
	}
	r := newTestResult(expected)

	ctx := t.Context()

	_, err1 := r.Descriptor(ctx)
	require.ErrorIs(t, err1, assert.AnError)

	// Cached error is returned on second call.
	_, err2 := r.Descriptor(ctx)
	require.ErrorIs(t, err2, assert.AnError)
}

func TestDescriptor_ContextCancellationDoesNotPoisonCache(t *testing.T) {
	expected := descriptorOrError{
		Descriptor: ociImageSpecV1.Descriptor{
			MediaType: "application/vnd.test",
			Digest:    digest.FromString("test"),
			Size:      42,
		},
	}

	// Channel is empty — first caller with cancelled context should fail
	// without caching.
	ch := make(chan descriptorOrError, 1)
	r := &Result{desc: ch}

	cancelledCtx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := r.Descriptor(cancelledCtx)
	require.ErrorIs(t, err, context.Canceled)

	// Cache should NOT be poisoned — send the real result now.
	ch <- expected

	// Second call with a valid context should succeed.
	desc, err := r.Descriptor(t.Context())
	require.NoError(t, err)
	assert.Equal(t, expected.Descriptor, desc)
}

func TestDescriptor_ConcurrentCallsReturnSameResult(t *testing.T) {
	expected := descriptorOrError{
		Descriptor: ociImageSpecV1.Descriptor{
			MediaType: "application/vnd.test",
			Digest:    digest.FromString("concurrent"),
			Size:      99,
		},
	}
	r := newTestResult(expected)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	results := make([]ociImageSpecV1.Descriptor, goroutines)
	errs := make([]error, goroutines)

	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = r.Descriptor(t.Context())
		}(i)
	}

	wg.Wait()

	for i := range goroutines {
		require.NoError(t, errs[i], "goroutine %d", i)
		assert.Equal(t, expected.Descriptor, results[i], "goroutine %d", i)
	}
}

func TestDescriptor_BlocksUntilValueAvailable(t *testing.T) {
	ch := make(chan descriptorOrError, 1)
	r := &Result{desc: ch}

	expected := ociImageSpecV1.Descriptor{
		MediaType: "application/vnd.test",
		Digest:    digest.FromString("delayed"),
		Size:      1,
	}

	// Send the result after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		ch <- descriptorOrError{Descriptor: expected}
	}()

	desc, err := r.Descriptor(t.Context())
	require.NoError(t, err)
	assert.Equal(t, expected, desc)
}
