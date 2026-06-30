package maven_test

import (
	"strings"
	"testing"

	coordinates "ocm.software/open-component-model/bindings/go/maven/internal/maven"
)

// sha1("1") == 356a192b7913b04c54574d18c28d46e6395428ab (well-known vector).
const oneSHA1 = "356a192b7913b04c54574d18c28d46e6395428ab"

func TestSHA1Hex(t *testing.T) {
	if got := coordinates.SHA1Hex([]byte("1")); got != oneSHA1 {
		t.Fatalf("SHA1Hex = %q, want %q", got, oneSHA1)
	}
}

func TestParseSHA1File(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"digest only", oneSHA1, oneSHA1},
		{"digest with filename", oneSHA1 + "  lib-1.0.0.jar", oneSHA1},
		{"trailing newline", oneSHA1 + "\n", oneSHA1},
		{"uppercase", strings.ToUpper(oneSHA1), oneSHA1},
		{"empty", "   \n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := coordinates.ParseSHA1File([]byte(c.in)); got != c.want {
				t.Fatalf("ParseSHA1File(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestVerifySHA1(t *testing.T) {
	data := []byte("1")
	t.Run("match", func(t *testing.T) {
		if err := coordinates.VerifySHA1(data, oneSHA1); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("match uppercase", func(t *testing.T) {
		if err := coordinates.VerifySHA1(data, strings.ToUpper(oneSHA1)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("mismatch", func(t *testing.T) {
		if err := coordinates.VerifySHA1(data, "deadbeef"); err == nil {
			t.Fatal("expected mismatch error, got nil")
		}
	})
}
