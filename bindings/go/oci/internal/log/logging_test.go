package log

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"

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
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == "time" {
				return slog.Attr{}
			}
			return a
		}})))

	done := Operation(ctx, "test-operation", slog.String("test", "value"))
	assert.Equal(t, "level=DEBUG msg=\"operation starting\" realm=oci operation=test-operation test=value\n", buf.String())
	buf.Reset()
	done(nil) // No error
	assert.Contains(t, buf.String(), "level=DEBUG msg=\"operation completed\" realm=oci operation=test-operation")
	buf.Reset()
	done(assert.AnError) // With error
	assert.Contains(t, buf.String(), "level=ERROR msg=\"operation failed\" realm=oci operation=test-operation")
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

func TestLogDefer(t *testing.T) {
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == "time" {
			return slog.Attr{}
		}
		return a
	}})))
	test := func() (err error) {
		done := Operation(t.Context(), "test")
		defer func() {
			done(err)
		}()
		return errors.New("operation failed")
	}
	_ = test()

	assert.Contains(t, buf.String(), "level=ERROR msg=\"operation failed\" realm=oci operation=test")
}
