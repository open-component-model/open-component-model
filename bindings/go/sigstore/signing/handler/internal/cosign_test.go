package internal

import (
	"debug/buildinfo"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveBinary_CosignOnPathFIPSBuild(t *testing.T) {
	withSettings := func(settings ...debug.BuildSetting) func(string) (*buildinfo.BuildInfo, error) {
		return func(string) (*buildinfo.BuildInfo, error) { return &buildinfo.BuildInfo{Settings: settings}, nil }
	}
	tests := []struct {
		name      string
		fips      bool
		readInfo  func(string) (*buildinfo.BuildInfo, error)
		wantErr   string
		wantReads bool
	}{
		{name: "FIPS build", fips: true, readInfo: withSettings(debug.BuildSetting{Key: "GOFIPS140", Value: "v1.26.0"}), wantReads: true},
		{name: "GOFIPS140=latest", fips: true, readInfo: withSettings(debug.BuildSetting{Key: "GOFIPS140", Value: "latest"}), wantErr: "built with GOFIPS140=latest", wantReads: true},
		{name: "no GOFIPS140", fips: true, readInfo: withSettings(debug.BuildSetting{Key: "CGO_ENABLED", Value: "0"}), wantErr: "built without GOFIPS140", wantReads: true},
		{name: "unreadable build info", fips: true, readInfo: func(string) (*buildinfo.BuildInfo, error) { return nil, errors.New("not a Go binary") }, wantErr: "not a Go binary", wantReads: true},
		{name: "non-FIPS build outside FIPS mode", readInfo: withSettings()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			var reads bool
			b := NewCosignBinary()
			b.LookPath = func(string) (string, error) { return "/fake/bin/cosign", nil }
			b.FIPSEnabled = func() bool { return tc.fips }
			b.ReadBuildInfo = func(p string) (*buildinfo.BuildInfo, error) { reads = true; return tc.readInfo(p) }

			path, err := b.resolveBinary(t.Context())
			r.Equal(tc.wantReads, reads)
			if tc.wantErr != "" {
				r.ErrorIs(err, ErrCosignNotFIPSBuild)
				r.ErrorContains(err, tc.wantErr)
				r.Empty(b.binaryPath, "a rejected cosign must not be cached")
				return
			}
			r.NoError(err)
			r.Equal("/fake/bin/cosign", path)
		})
	}
}

// recordingTransport fails every request and records that one was made.
type recordingTransport struct{ called bool }

func (rt *recordingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	rt.called = true
	return nil, errors.New("network disabled in test")
}

func TestResolveBinary_NoCosignOnPath(t *testing.T) {
	tests := []struct {
		name         string
		fips         bool
		wantFIPSErr  bool
		wantDownload bool
	}{
		{name: "FIPS mode refuses to download", fips: true, wantFIPSErr: true},
		{name: "non-FIPS mode attempts the download", fips: false, wantDownload: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			// Isolate the download cache so a previously cached cosign cannot
			// short-circuit the download attempt.
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())

			transport := &recordingTransport{}
			b := NewCosignBinary()
			b.HttpClient = &http.Client{Transport: transport}
			b.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
			b.FIPSEnabled = func() bool { return tc.fips }

			_, err := b.resolveBinary(t.Context())
			r.Error(err)
			r.Equal(tc.wantFIPSErr, errors.Is(err, ErrCosignDownloadInFIPSMode))
			r.Equal(tc.wantDownload, transport.called)
		})
	}
}

func TestHasEnvKey(t *testing.T) {
	r := require.New(t)
	t.Setenv("SIGSTORE_ID_TOKEN", "some-token")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "ghs_fakeRunnerToken")
	env := os.Environ()
	r.True(HasEnvKey(env, "SIGSTORE_ID_TOKEN"))
	r.True(HasEnvKey(env, "ACTIONS_ID_TOKEN_REQUEST_TOKEN"))
}

func TestHasEnvKey_EmptyValueTreatedAsAbsent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	env := []string{"SIGSTORE_ID_TOKEN=", "OTHER_KEY=value"}
	r.False(HasEnvKey(env, "SIGSTORE_ID_TOKEN"))
	r.True(HasEnvKey(env, "OTHER_KEY"))
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
