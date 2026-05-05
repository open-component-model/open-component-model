package handler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
)

func TestCosignEnv(t *testing.T) {
	t.Run("passes through standard and sigstore env vars", func(t *testing.T) {
		r := require.New(t)
		t.Setenv("PATH", "/usr/bin")
		t.Setenv("HOME", "/home/test")
		t.Setenv("HTTPS_PROXY", "https://proxy.example.com")
		t.Setenv("SIGSTORE_ID_TOKEN", "some-token")
		t.Setenv("COSIGN_EXPERIMENTAL", "1")
		t.Setenv("TUF_ROOT", "/tmp/tuf")
		env := cosignEnv()
		r.True(hasEnvKey(env, "PATH"))
		r.True(hasEnvKey(env, "HOME"))
		r.True(hasEnvKey(env, "HTTPS_PROXY"))
		r.True(hasEnvKey(env, "SIGSTORE_ID_TOKEN"))
		r.True(hasEnvKey(env, "COSIGN_EXPERIMENTAL"))
		r.True(hasEnvKey(env, "TUF_ROOT"))
	})

	t.Run("excludes library injection vectors", func(t *testing.T) {
		r := require.New(t)
		denied := map[string]string{
			"LD_PRELOAD":            "/tmp/evil.so",
			"DYLD_INSERT_LIBRARIES": "/tmp/evil.dylib",
			"LD_LIBRARY_PATH":       "/tmp/lib",
			"BASH_ENV":              "/tmp/evil.sh",
		}
		for k, v := range denied {
			t.Setenv(k, v)
		}
		env := cosignEnv()
		for k := range denied {
			r.False(hasEnvKey(env, k), "expected %s to be excluded", k)
		}
	})
}

func TestSignConfigValidateHTTPS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, url, wantErr string
	}{
		{"empty is valid", "", ""},
		{"valid https", "https://example.com/path", ""},
		{"http rejected", "http://example.com", "must use https scheme"},
		{"no host", "https://", "has no host"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			cfg := &v1alpha1.SignConfig{FulcioURL: tc.url}
			err := cfg.Validate()
			if tc.wantErr == "" {
				r.NoError(err)
			} else {
				r.ErrorContains(err, tc.wantErr)
			}
		})
	}
}

func TestSignConfigValidateAllowInsecure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	cfg := &v1alpha1.SignConfig{
		FulcioURL: "http://fulcio.local:8080", RekorURL: "http://rekor.local:3000",
		TimestampServerURL: "http://tsa.local:5555", AllowInsecureEndpoints: false,
	}
	r.Error(cfg.Validate())
	cfg.AllowInsecureEndpoints = true
	r.NoError(cfg.Validate())
}

func TestVerifyConfigValidateAllowInsecure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	cfg := &v1alpha1.VerifyConfig{
		CertificateOIDCIssuer:  "http://issuer.local",
		CertificateIdentity:    "user@example.com",
		AllowInsecureEndpoints: false,
	}
	r.Error(cfg.Validate())
	cfg.AllowInsecureEndpoints = true
	r.NoError(cfg.Validate())
}

func TestParseCosignVersionOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, input, want string
		wantErr           bool
	}{
		{"GitVersion line", "GitVersion:    v3.0.6\n", "v3.0.6", false},
		{"version in other format", "cosign v3.0.3 (linux/amd64)\n", "v3.0.3", false},
		{"no version found", "some random output", "", true},
		{"empty string", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			got, err := parseCosignVersionOutput(tc.input)
			if tc.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, got)
		})
	}
}
