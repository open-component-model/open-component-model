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

// setMode sets the FIPS 140-3 mode seen by b: "off", "on" (fips140=on) or "only" (fips140=only).
func setMode(b *CosignBinary, mode string) {
	b.FIPSEnabled = func() bool { return mode != "off" }
	b.FIPSEnforced = func() bool { return mode == "only" }
}

func TestResolveBinary_CosignOnPathFIPSBuild(t *testing.T) {
	withSettings := func(settings ...debug.BuildSetting) func(string) (*buildinfo.BuildInfo, error) {
		return func(string) (*buildinfo.BuildInfo, error) { return &buildinfo.BuildInfo{Settings: settings}, nil }
	}
	fipsBuild := withSettings(debug.BuildSetting{Key: "GOFIPS140", Value: "v1.26.0"})
	plainBuild := withSettings(debug.BuildSetting{Key: "CGO_ENABLED", Value: "0"})
	tests := []struct {
		name      string
		mode      string
		readInfo  func(string) (*buildinfo.BuildInfo, error)
		wantErr   string
		wantReads bool
	}{
		{name: "only: FIPS build accepted", mode: "only", readInfo: fipsBuild, wantReads: true},
		{name: "only: GOFIPS140=latest rejected", mode: "only", readInfo: withSettings(debug.BuildSetting{Key: "GOFIPS140", Value: "latest"}), wantErr: "built with GOFIPS140=latest", wantReads: true},
		{name: "only: no GOFIPS140 rejected", mode: "only", readInfo: plainBuild, wantErr: "built without GOFIPS140", wantReads: true},
		{name: "only: unreadable build info rejected", mode: "only", readInfo: func(string) (*buildinfo.BuildInfo, error) { return nil, errors.New("not a Go binary") }, wantErr: "not a Go binary", wantReads: true},
		{name: "on: non-FIPS build accepted after the check", mode: "on", readInfo: plainBuild, wantReads: true},
		{name: "off: build information not read", mode: "off", readInfo: plainBuild},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			var reads bool
			b := NewCosignBinary()
			b.LookPath = func(string) (string, error) { return "/fake/bin/cosign", nil }
			setMode(b, tc.mode)
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
		mode         string
		wantFIPSErr  bool
		wantDownload bool
	}{
		{mode: "only", wantFIPSErr: true},
		{mode: "on", wantDownload: true},
		{mode: "off", wantDownload: true},
	}
	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			r := require.New(t)
			// Isolate the download cache so a previously cached cosign cannot
			// short-circuit the download attempt.
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())

			transport := &recordingTransport{}
			b := NewCosignBinary()
			b.HttpClient = &http.Client{Transport: transport}
			b.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
			setMode(b, tc.mode)

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
