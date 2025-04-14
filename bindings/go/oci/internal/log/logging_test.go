package log

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestOperation(t *testing.T) {
	ctx := t.Context()
	def := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(def)
	})
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == "time" {
			return slog.Time("time", time.Time{})
		}
		return a
	}})))

	done := Operation(ctx, "test-operation", slog.String("test", "value"))
	assert.Equal(t, "time=0001-01-01T00:00:00.000Z level=INFO msg=\"INFO starting operation realm=oci operation=test-operation test=value\"\n", buf.String())
	buf.Reset()
	done(nil) // No error
	assert.Contains(t, buf.String(), "time=0001-01-01T00:00:00.000Z level=INFO msg=\"INFO operation completed realm=oci operation=test-operation test=value")
	buf.Reset()
	done(assert.AnError) // With error
	assert.Contains(t, buf.String(), "time=0001-01-01T00:00:00.000Z level=INFO msg=\"ERROR operation failed realm=oci operation=test-operation test=value")
}

func TestDescriptorLogAttr(t *testing.T) {
	descriptor := ociImageSpecV1.Descriptor{
		MediaType:    "application/vnd.oci.image.manifest.v1+json",
		Digest:       "sha256:1234567890abcdef",
		Size:         1024,
		ArtifactType: "test-artifact",
	}

	attr := DescriptorLogAttr(descriptor)
	if attr.Key != "descriptor" {
		t.Errorf("expected key 'descriptor', got %s", attr.Key)
	}

	value := attr.Value.Any()
	group, ok := value.([]slog.Attr)
	if !ok {
		t.Fatal("expected []slog.Attr")
	}
	if len(group) != 4 {
		t.Errorf("expected 4 attributes, got %d", len(group))
	}
}

func TestIdentityLogAttr(t *testing.T) {
	identity := runtime.Identity{
		"name":    "test-name",
		"version": "1.0.0",
		"type":    "test-type",
	}

	attr := IdentityLogAttr("test-group", identity)
	if attr.Key != "test-group" {
		t.Errorf("expected key 'test-group', got %s", attr.Key)
	}

	value := attr.Value.Any()
	group, ok := value.([]slog.Attr)
	if !ok {
		t.Fatal("expected slog.Attr")
	}

	if len(group) != 3 {
		t.Errorf("expected 3 attributes, got %d", len(group))
	}

	// Verify sorted keys
	expectedKeys := []string{"name", "type", "version"}
	for i, attr := range group {
		if attr.Key != expectedKeys[i] {
			t.Errorf("expected key %s at position %d, got %s", expectedKeys[i], i, attr.Key)
		}
	}
}
