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

func reconcilerWithLimit(mib int64) *Reconciler {
	return &Reconciler{MaxResourceSizeMiB: mib}
}

func TestApplyResourceSizeLimit_Disabled(t *testing.T) {
	r := reconcilerWithLimit(0)
	original := nopCloser{bytes.NewReader([]byte("data"))}
	b := &mockBlob{data: []byte("data")}

	got, err := r.applyResourceSizeLimit(b, original)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != original {
		t.Fatal("expected original reader to be returned unchanged when limit is 0")
	}
}

func TestApplyResourceSizeLimit_SizeAware_UnderLimit(t *testing.T) {
	r := reconcilerWithLimit(1) // 1 MiB limit
	data := []byte("small")
	b := &mockSizeAwareBlob{mockBlob: mockBlob{data: data}, size: int64(len(data))}
	rc := nopCloser{bytes.NewReader(data)}

	limited, err := r.applyResourceSizeLimit(b, rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := limited.(*limitedReadCloser); !ok {
		t.Fatal("expected a *limitedReadCloser wrapper")
	}
}

func TestApplyResourceSizeLimit_SizeAware_OverLimit(t *testing.T) {
	r := reconcilerWithLimit(1)                    // 1 MiB limit
	data := bytes.Repeat([]byte("x"), 2*1024*1024) // 2 MiB
	b := &mockSizeAwareBlob{mockBlob: mockBlob{data: data}, size: int64(len(data))}
	rc := nopCloser{bytes.NewReader(data)}

	got, err := r.applyResourceSizeLimit(b, rc)
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

func TestApplyResourceSizeLimit_SizeAware_SizeUnknown(t *testing.T) {
	r := reconcilerWithLimit(1)
	data := []byte("small")
	b := &mockSizeAwareBlob{mockBlob: mockBlob{data: data}, size: blob.SizeUnknown}
	rc := nopCloser{bytes.NewReader(data)}

	limited, err := r.applyResourceSizeLimit(b, rc)
	if err != nil {
		t.Fatalf("unexpected error for SizeUnknown blob: %v", err)
	}
	if _, ok := limited.(*limitedReadCloser); !ok {
		t.Fatal("expected a *limitedReadCloser wrapper when size is unknown")
	}
}

func TestApplyResourceSizeLimit_NonSizeAware(t *testing.T) {
	r := reconcilerWithLimit(1)
	data := []byte("small")
	b := &mockBlob{data: data}
	rc := nopCloser{bytes.NewReader(data)}

	limited, err := r.applyResourceSizeLimit(b, rc)
	if err != nil {
		t.Fatalf("unexpected error for non-SizeAware blob: %v", err)
	}
	if _, ok := limited.(*limitedReadCloser); !ok {
		t.Fatal("expected a *limitedReadCloser wrapper for non-SizeAware blob")
	}
}
