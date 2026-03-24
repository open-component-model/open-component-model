package deployer

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"ocm.software/open-component-model/bindings/go/blob"
)

// readAll reads all bytes from r, returning the data and any non-EOF error.
func readAll(t *testing.T, r io.Reader) ([]byte, error) {
	t.Helper()
	var buf bytes.Buffer
	_, err := io.Copy(&buf, r)
	return buf.Bytes(), err
}

// nopCloser wraps an io.Reader with a no-op Close.
type nopCloser struct{ io.Reader }

func (nopCloser) Close() error { return nil }

// mockBlob implements blob.ReadOnlyBlob backed by a byte slice.
type mockBlob struct {
	data []byte
}

func (m *mockBlob) ReadCloser() (io.ReadCloser, error) {
	return nopCloser{bytes.NewReader(m.data)}, nil
}

// mockSizeAwareBlob additionally implements blob.SizeAware.
type mockSizeAwareBlob struct {
	mockBlob
	size int64
}

func (m *mockSizeAwareBlob) Size() int64 { return m.size }

func TestLimitedReadCloser_UnderLimit(t *testing.T) {
	data := []byte("hello")
	rc := &limitedReadCloser{
		Closer:  nopCloser{},
		limited: &io.LimitedReader{R: bytes.NewReader(data), N: 10},
	}

	got, err := readAll(t, rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestLimitedReadCloser_ExactlyAtLimit(t *testing.T) {
	data := []byte("hello")
	rc := &limitedReadCloser{
		Closer:  nopCloser{},
		limited: &io.LimitedReader{R: bytes.NewReader(data), N: int64(len(data))},
	}

	got, err := readAll(t, rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestLimitedReadCloser_OneByteOverLimit(t *testing.T) {
	data := []byte("hello!")
	rc := &limitedReadCloser{
		Closer:  nopCloser{},
		limited: &io.LimitedReader{R: bytes.NewReader(data), N: int64(len(data)) - 1},
	}

	_, err := readAll(t, rc)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum allowed size") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLimitedReadCloser_MultipleReadsAtLimit(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 100)
	rc := &limitedReadCloser{
		Closer:  nopCloser{},
		limited: &io.LimitedReader{R: bytes.NewReader(data), N: int64(len(data))},
	}

	// Read in small chunks
	buf := make([]byte, 7)
	var collected []byte
	for {
		n, err := rc.Read(buf)
		collected = append(collected, buf[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if len(collected) != len(data) {
		t.Fatalf("got %d bytes, want %d", len(collected), len(data))
	}
}

func TestGetLimitedReader_Disabled(t *testing.T) {
	b := &mockBlob{data: []byte("data")}

	got, err := getLimitedReader(b, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(*limitedReadCloser); ok {
		t.Fatal("expected unwrapped reader when limit is 0")
	}
}

func TestGetLimitedReader_SizeAware_UnderLimit(t *testing.T) {
	data := []byte("small")
	b := &mockSizeAwareBlob{mockBlob: mockBlob{data: data}, size: int64(len(data))}

	limited, err := getLimitedReader(b, 1) // 1 MiB limit
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := limited.(*limitedReadCloser); !ok {
		t.Fatal("expected a *limitedReadCloser wrapper")
	}
}

func TestGetLimitedReader_SizeAware_OverLimit(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 2*1024*1024) // 2 MiB
	b := &mockSizeAwareBlob{mockBlob: mockBlob{data: data}, size: int64(len(data))}

	got, err := getLimitedReader(b, 1) // 1 MiB limit
	if got != nil {
		t.Fatal("expected nil reader when pre-check rejects")
	}
	if err == nil {
		t.Fatal("expected error from pre-check")
	}
	if !strings.Contains(err.Error(), "exceeds maximum allowed size") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestGetLimitedReader_SizeAware_SizeUnknown(t *testing.T) {
	data := []byte("small")
	b := &mockSizeAwareBlob{mockBlob: mockBlob{data: data}, size: blob.SizeUnknown}

	limited, err := getLimitedReader(b, 1)
	if err != nil {
		t.Fatalf("unexpected error for SizeUnknown blob: %v", err)
	}
	if _, ok := limited.(*limitedReadCloser); !ok {
		t.Fatal("expected a *limitedReadCloser wrapper when size is unknown")
	}
}

func TestGetLimitedReader_NonSizeAware(t *testing.T) {
	data := []byte("small")
	b := &mockBlob{data: data}

	limited, err := getLimitedReader(b, 1)
	if err != nil {
		t.Fatalf("unexpected error for non-SizeAware blob: %v", err)
	}
	if _, ok := limited.(*limitedReadCloser); !ok {
		t.Fatal("expected a *limitedReadCloser wrapper for non-SizeAware blob")
	}
}

// TestGetLimitedReader_NegativeLimit documents the behavior when maxResourceSizeMiB is
// negative. Negative values are rejected at process startup (cmd/main.go), so this path is not
// reachable in production. The behavior is determined by io.LimitedReader: a negative N causes it
// to return 0, io.EOF on the very first Read, so no data passes through.
func TestGetLimitedReader_NegativeLimit(t *testing.T) {
	data := []byte("hello")
	b := &mockBlob{data: data}

	limited, err := getLimitedReader(b, -1)
	if err != nil {
		t.Fatalf("unexpected error: getLimitedReader should not return an error for negative limit, got: %v", err)
	}
	if _, ok := limited.(*limitedReadCloser); !ok {
		t.Fatalf("expected a *limitedReadCloser, got %T", limited)
	}

	// io.LimitedReader with N <= 0 returns 0, io.EOF immediately — no data passes through.
	buf := make([]byte, len(data))
	n, readErr := limited.Read(buf)
	if n != 0 {
		t.Fatalf("expected 0 bytes read with negative limit, got %d", n)
	}
	if readErr != io.EOF {
		t.Fatalf("expected io.EOF on first read with negative limit, got: %v", readErr)
	}
}
