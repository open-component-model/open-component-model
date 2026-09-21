package checksum

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// Digests of the ASCII payload "hello".
const (
	helloSHA256    = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	helloSHA1      = "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"
	helloMD5       = "5d41402abc4b2a76b9719d911017c592"
	helloSHA256B64 = "LPJNul+wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ="
	helloSHA1B64   = "qvTGHdzF6KLavt4PO0gs2a6pQ00="
)

func helloComputed() map[string]string {
	return map[string]string{
		SHA256.OCMName: helloSHA256,
		SHA1.OCMName:   helloSHA1,
		MD5.OCMName:    helloMD5,
	}
}

func TestFromHeaders_RFC9530ContentDigest(t *testing.T) {
	r := require.New(t)
	h := http.Header{}
	// Multiple algorithms in one Content-Digest dictionary.
	h.Set("Content-Digest", fmt.Sprintf("sha-256=:%s:,sha=:%s:", helloSHA256B64, helloSHA1B64))

	got := FromHeaders(h, nil)
	r.Len(got, 2)

	exp, ok := Select(got, []Algorithm{SHA256})
	r.True(ok)
	r.Equal("SHA-256", exp.Algorithm.OCMName)
	r.Equal(helloSHA256, exp.Value)

	exp, ok = Select(got, []Algorithm{SHA1})
	r.True(ok)
	r.Equal("SHA-1", exp.Algorithm.OCMName)
	r.Equal(helloSHA1, exp.Value)
}

func TestFromHeaders_LegacyXChecksum(t *testing.T) {
	r := require.New(t)
	h := http.Header{}
	h.Set("x-checksum-sha1", helloSHA1)
	h.Set("x-checksum-md5", helloMD5)

	got := FromHeaders(h, nil)
	r.Len(got, 2)

	// No preference => strongest first (SHA-1 before MD5).
	exp, ok := Select(got, nil)
	r.True(ok)
	r.Equal("SHA-1", exp.Algorithm.OCMName)
	r.Equal(helloSHA1, exp.Value)
}

func TestFromHeaders_ExtraHeaderInfersAlgorithm(t *testing.T) {
	r := require.New(t)
	h := http.Header{}
	h.Set("x-artifact-sha256", helloSHA256)

	got := FromHeaders(h, []string{"x-artifact-sha256"})
	r.Len(got, 1)
	r.Equal("SHA-256", got[0].Algorithm.OCMName)
	r.Equal(helloSHA256, got[0].Value)
}

func TestFromHeaders_IgnoresMalformedValues(t *testing.T) {
	r := require.New(t)
	h := http.Header{}
	h.Set("x-checksum-sha256", "not-a-hex-digest")
	h.Set("Content-Digest", "sha-256=:not-base64!:")
	r.Empty(FromHeaders(h, nil))
}

func TestParseChecksumFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		alg  Algorithm
		want string
		err  bool
	}{
		{name: "bare hex", body: helloSHA1 + "\n", alg: SHA1, want: helloSHA1},
		{name: "gnu coreutils", body: helloSHA256 + "  artifact.jar\n", alg: SHA256, want: helloSHA256},
		{name: "wrong length", body: "abcd\n", alg: SHA256, err: true},
		{name: "empty", body: "\n\n", alg: SHA256, err: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseChecksumFile(tc.body, tc.alg)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestVerify(t *testing.T) {
	r := require.New(t)
	computed := helloComputed()

	r.NoError(Verify(computed, Expected{Algorithm: SHA256, Value: helloSHA256}))
	// Case-insensitive comparison.
	r.NoError(Verify(computed, Expected{Algorithm: SHA1, Value: "AAF4C61DDCC5E8A2DABEDE0F3B482CD9AEA9434D"}))
	// Mismatch is an error.
	r.Error(Verify(computed, Expected{Algorithm: SHA256, Value: helloSHA1 + "00"}))
	// Missing computed algorithm is an error.
	r.Error(Verify(map[string]string{}, Expected{Algorithm: SHA256, Value: helloSHA256}))
}

func TestResolve_HeaderMatchAndVerify(t *testing.T) {
	r := require.New(t)
	h := http.Header{}
	h.Set("x-checksum-sha1", helloSHA1)

	exp, verified, err := Resolve(context.Background(), Policy{
		Sources:   []Source{{Type: SourceHTTPHeader}},
		OnMissing: Fail,
	}, Input{Headers: h, Computed: helloComputed()})
	r.NoError(err)
	r.True(verified)
	r.Equal("SHA-1", exp.Algorithm.OCMName)
}

func TestResolve_HeaderMismatchFails(t *testing.T) {
	r := require.New(t)
	h := http.Header{}
	h.Set("x-checksum-sha256", "0000000000000000000000000000000000000000000000000000000000000000") // valid length, wrong value

	_, _, err := Resolve(context.Background(), Policy{
		Sources: []Source{{Type: SourceHTTPHeader}},
	}, Input{Headers: h, Computed: helloComputed()})
	r.Error(err)
	r.Contains(err.Error(), "checksum mismatch")
}

func TestResolve_ExternalFallbackAfterHeaderMiss(t *testing.T) {
	r := require.New(t)
	// No usable header; external fetch supplies a SHA-256.
	fetch := func(_ context.Context, _ string, alg Algorithm) (Expected, bool, error) {
		if alg.OCMName != "SHA-256" {
			return Expected{}, false, nil
		}
		return Expected{Algorithm: SHA256, Value: helloSHA256}, true, nil
	}
	exp, verified, err := Resolve(context.Background(), Policy{
		Sources: []Source{
			{Type: SourceHTTPHeader},
			{Type: SourceExternalURL},
		},
		OnMissing: Fail,
	}, Input{Headers: http.Header{}, Computed: helloComputed(), FetchURL: fetch})
	r.NoError(err)
	r.True(verified)
	r.Equal("SHA-256", exp.Algorithm.OCMName)
}

func TestResolve_OnMissingFail(t *testing.T) {
	r := require.New(t)
	_, _, err := Resolve(context.Background(), Policy{
		Sources:   []Source{{Type: SourceHTTPHeader}},
		OnMissing: Fail,
	}, Input{Headers: http.Header{}, Computed: helloComputed()})
	r.Error(err)
	r.Contains(err.Error(), "no checksum could be obtained")
}

func TestResolve_OnMissingCompute(t *testing.T) {
	r := require.New(t)
	_, verified, err := Resolve(context.Background(), Policy{
		Sources:   []Source{{Type: SourceHTTPHeader}},
		OnMissing: Compute,
	}, Input{Headers: http.Header{}, Computed: helloComputed()})
	r.NoError(err)
	r.False(verified, "no external checksum was verified")
}

func TestResolve_StreamSourceStops(t *testing.T) {
	r := require.New(t)
	// A stream source short-circuits without verification even if headers exist.
	h := http.Header{}
	h.Set("x-checksum-sha256", helloSHA1+"00") // would mismatch if consulted
	_, verified, err := Resolve(context.Background(), Policy{
		Sources: []Source{{Type: SourceStream}, {Type: SourceHTTPHeader}},
	}, Input{Headers: h, Computed: helloComputed()})
	r.NoError(err)
	r.False(verified)
}

func TestRequiredAlgorithms_AlwaysIncludesStorage(t *testing.T) {
	r := require.New(t)
	got := RequiredAlgorithms(Policy{Sources: []Source{{Type: SourceExternalURL, Algorithms: []Algorithm{SHA1}}}})
	names := map[string]bool{}
	for _, a := range got {
		names[a.OCMName] = true
	}
	r.True(names["SHA-256"], "storage algorithm must always be computed")
	r.True(names["SHA-1"], "policy algorithm must be computed")
}

// TestResolve_ExternalDefaultURL confirms that a nil Source.ResolveURL falls back
// to the Maven default `<baseURL>.<alg.Extension>`, and that FetchURL sees that
// exact URL for each algorithm.
func TestResolve_ExternalDefaultURL(t *testing.T) {
	r := require.New(t)
	seen := map[string]string{}
	fetch := func(_ context.Context, u string, alg Algorithm) (Expected, bool, error) {
		seen[alg.OCMName] = u
		if alg.OCMName == "SHA-256" {
			return Expected{Algorithm: SHA256, Value: helloSHA256}, true, nil
		}
		return Expected{}, false, nil
	}
	_, verified, err := Resolve(context.Background(), Policy{
		Sources: []Source{{Type: SourceExternalURL, Algorithms: []Algorithm{SHA1, SHA256}}},
	}, Input{URL: "https://example.com/artifact.tar", Computed: helloComputed(), FetchURL: fetch})
	r.NoError(err)
	r.True(verified)
	r.Equal("https://example.com/artifact.tar.sha1", seen["SHA-1"])
	r.Equal("https://example.com/artifact.tar.sha256", seen["SHA-256"])
}

// TestResolve_ExternalCustomResolveURL confirms that a Source.ResolveURL closure
// is honored per algorithm, so a policy can point at a non-sibling checksum URL
// without any templating language leaking into the checksum package.
func TestResolve_ExternalCustomResolveURL(t *testing.T) {
	r := require.New(t)
	var seen []string
	fetch := func(_ context.Context, u string, alg Algorithm) (Expected, bool, error) {
		seen = append(seen, u)
		if alg.OCMName == "SHA-256" {
			return Expected{Algorithm: SHA256, Value: helloSHA256}, true, nil
		}
		return Expected{}, false, nil
	}
	resolveURL := func(baseURL string, alg Algorithm) (string, error) {
		return "https://mirror.example/checksums/" + alg.Extension + "?src=" + baseURL, nil
	}
	_, verified, err := Resolve(context.Background(), Policy{
		Sources: []Source{{
			Type:       SourceExternalURL,
			Algorithms: []Algorithm{SHA1, SHA256},
			ResolveURL: resolveURL,
		}},
	}, Input{URL: "https://example.com/artifact.tar", Computed: helloComputed(), FetchURL: fetch})
	r.NoError(err)
	r.True(verified)
	r.Equal([]string{
		"https://mirror.example/checksums/sha1?src=https://example.com/artifact.tar",
		"https://mirror.example/checksums/sha256?src=https://example.com/artifact.tar",
	}, seen)
}

// TestResolve_ExternalResolveURLError surfaces a resolver error as a hard failure
// rather than falling through to onMissing behaviour.
func TestResolve_ExternalResolveURLError(t *testing.T) {
	r := require.New(t)
	fetch := func(context.Context, string, Algorithm) (Expected, bool, error) {
		t.Fatal("FetchURL must not be called when ResolveURL fails")
		return Expected{}, false, nil
	}
	resolveURL := func(string, Algorithm) (string, error) {
		return "", fmt.Errorf("boom")
	}
	_, _, err := Resolve(context.Background(), Policy{
		Sources: []Source{{Type: SourceExternalURL, Algorithms: []Algorithm{SHA256}, ResolveURL: resolveURL}},
	}, Input{URL: "https://example.com/artifact", Computed: helloComputed(), FetchURL: fetch})
	r.Error(err)
	r.Contains(err.Error(), "boom")
}
