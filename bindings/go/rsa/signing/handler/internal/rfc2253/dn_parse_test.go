package rfc2253_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	dn "ocm.software/open-component-model/bindings/go/rsa/signing/handler/internal/rfc2253"
)

func TestParse_Plain(t *testing.T) {
	got, err := dn.Parse("open-component-model")
	require.NoError(t, err)
	require.Equal(t, "CN=open-component-model", got.String())
}

func TestParse_SingleField(t *testing.T) {
	got, err := dn.Parse("CN=open-component-model")
	require.NoError(t, err)
	require.Equal(t, "CN=open-component-model", got.String())
}

func TestParse_TwoFields(t *testing.T) {
	got, err := dn.Parse("CN=open-component-model,C=DE")
	require.NoError(t, err)
	require.Equal(t, "CN=open-component-model,C=DE", got.String())
}

func TestParse_ThreeFields(t *testing.T) {
	got, err := dn.Parse("CN=open-component-model,C=DE,ST=BW")
	require.NoError(t, err)
	require.Equal(t, "CN=open-component-model,ST=BW,C=DE", got.String())
}

func TestParse_DoubleFields_PlusAfter(t *testing.T) {
	got, err := dn.Parse("CN=open-component-model,C=DE+C=US")
	require.NoError(t, err)
	require.Equal(t, "CN=open-component-model,C=DE+C=US", got.String())
}

func TestParse_DoubleFields_PlusBefore(t *testing.T) {
	got, err := dn.Parse("C=DE+C=US,CN=open-component-model")
	require.NoError(t, err)
	require.Equal(t, "CN=open-component-model,C=DE+C=US", got.String())
}

func TestParse_DoubleFields_WithOthers(t *testing.T) {
	got, err := dn.Parse("C=DE+C=US,CN=open-component-model,L=Walldorf,O=open-component-model")
	require.NoError(t, err)
	require.Equal(t, "CN=open-component-model,O=open-component-model,L=Walldorf,C=DE+C=US", got.String())
}

func TestParse_WithOptions(t *testing.T) {
	t.Parallel()

	t.Run("Strict rejects unknown", func(t *testing.T) {
		_, err := dn.ParseWithOptions("FOO=bar", dn.Options{Strict: true})
		if err == nil {
			t.Fatalf("expected error for unknown attribute in strict mode")
		}
	})

	t.Run("NonStrict allows unknown but drops it", func(t *testing.T) {
		got, err := dn.ParseWithOptions("FOO=bar", dn.Options{Strict: false})
		require.NoError(t, err)
		if len(got.ExtraNames) != 0 {
			t.Fatalf("expected no ExtraNames, got %+v", got.ExtraNames)
		}
	})

	t.Run("Fallback disabled leaves empty Name", func(t *testing.T) {
		got, err := dn.ParseWithOptions("   ", dn.Options{FallbackToCN: false})
		if err == nil {
			t.Fatalf("expected error for empty DN")
		}

		// Case: non-empty input without AVAs
		got, err = dn.ParseWithOptions("plainstring", dn.Options{FallbackToCN: false})
		require.NoError(t, err)
		if got.CommonName != "" {
			t.Fatalf("expected empty CommonName, got %q", got.CommonName)
		}
	})

	t.Run("Fallback enabled puts whole string into CN", func(t *testing.T) {
		got, err := dn.ParseWithOptions("plainstring", dn.Options{FallbackToCN: true})
		require.NoError(t, err)
		if got.CommonName != "plainstring" {
			t.Fatalf("expected fallback CN, got %q", got.CommonName)
		}
	})
}
