package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	accessv1 "ocm.software/open-component-model/bindings/go/npm/spec/access/v1"
	credv1 "ocm.software/open-component-model/bindings/go/npm/spec/credentials/v1"
)

type authTransport func(*http.Request) (*http.Response, error)

func (f authTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAuthDifferentPortRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("credentials leaked: %q", got)
		}
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("initial credentials missing")
		}
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()
	resp, err := get(t.Context(), source.URL, &credv1.NPMCredentials{Token: "private-token"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAuthRedirectOrigins(t *testing.T) {
	for _, target := range []string{"https://registry.example:443/next", "https://sub.registry.example/next", "https://registry.example:444/next", "https://other.example/next", "http://registry.example/next"} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: authTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": {target}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				want := ""
				if target == "https://registry.example:443/next" {
					want = "Bearer private-token"
				}
				if got := r.Header.Get("Authorization"); got != want {
					t.Errorf("Authorization = %q, want %q", got, want)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
			})}
			resp, err := get(t.Context(), "https://registry.example/start", &credv1.NPMCredentials{Token: "private-token"}, Options{Client: client})
			if strings.HasPrefix(target, "http:") {
				if err == nil || calls != 1 {
					t.Fatalf("downgrade: calls=%d err=%v", calls, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := resp.Body.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAuthCallerRedirectPolicy(t *testing.T) {
	sentinel := errors.New("caller rejected redirect")
	for _, policyErr := range []error{http.ErrUseLastResponse, sentinel, nil} {
		t.Run(fmt.Sprint(policyErr), func(t *testing.T) {
			calls, callbacks := 0, 0
			client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
				callbacks++
				if r.Header.Get("Authorization") != "" {
					t.Error("callback saw cross-origin credentials")
				}
				// Security must also hold after a caller modifies the request.
				r.Header.Set("Authorization", "caller-secret")
				r.URL.User = url.UserPassword("caller-user", "caller-password")
				return policyErr
			}, Transport: authTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://sub.registry.example/next"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				if r.Header.Get("Authorization") != "" || r.URL.User != nil {
					t.Error("callback credentials leaked")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
			})}
			resp, err := get(t.Context(), "https://registry.example/start", &credv1.NPMCredentials{Token: "private-token"}, Options{Client: client})
			if policyErr == sentinel {
				if !errors.Is(err, sentinel) {
					t.Fatalf("lost caller error: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := resp.Body.Close(); err != nil {
						t.Error(err)
					}
				})
				if policyErr == http.ErrUseLastResponse && resp.StatusCode != 302 {
					t.Fatalf("status = %d", resp.StatusCode)
				}
			}
			if callbacks != 1 {
				t.Fatalf("callbacks = %d", callbacks)
			}
			if policyErr != nil && calls != 1 {
				t.Fatalf("unexpected follow: %d", calls)
			}
		})
	}
}

func TestAuthRedirectChainStaysBoundToInitialOrigin(t *testing.T) {
	locations := []string{"https://sub.registry.example/one", "https://sub.registry.example/two"}
	calls := 0
	client := &http.Client{Transport: authTransport(func(r *http.Request) (*http.Response, error) {
		if calls > 0 && r.Header.Get("Authorization") != "" {
			t.Error("credentials leaked on redirected hop")
		}
		resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}
		if calls < len(locations) {
			resp.StatusCode = http.StatusFound
			resp.Header.Set("Location", locations[calls])
		}
		calls++
		return resp, nil
	})}
	resp, err := get(t.Context(), "https://registry.example/start", &credv1.NPMCredentials{Token: "private-token"}, Options{Client: client})
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestAuthDefaultRedirectLimit(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: authTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"/loop"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	_, err := get(t.Context(), "https://registry.example/start", &credv1.NPMCredentials{Token: "private-token"}, Options{Client: client})
	if err == nil || calls != maxRedirects {
		t.Fatalf("calls = %d, error = %v", calls, err)
	}
	if client.CheckRedirect != nil {
		t.Fatal("modified caller's client")
	}
}

func TestAuthRedaction(t *testing.T) {
	raw := "https://private-user:private-password@registry.example/a?token=private-query#private-fragment"
	cause := &url.Error{Op: "Get", URL: raw, Err: context.Canceled}
	err := redact(fmt.Errorf("outer: %w", cause))
	assertAuthNoSecrets(t, err.Error())
	if !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation")
	}
	var got *url.Error
	if !errors.As(err, &got) || got != cause {
		t.Fatal("lost original URL error")
	}
	for _, raw := range []string{raw, "https://private-user:private-password@registry.example/%zz?token=private-query", "/relative?token=private-query"} {
		assertAuthNoSecrets(t, safeURLString(raw))
		assertAuthNoSecrets(t, redact(&url.Error{Op: "parse", URL: raw, Err: errors.New("failure")}).Error())
	}
	assertAuthNoSecrets(t, redact(errors.New(`bad Location: "https://private-user:private-password@registry.example/a?token=private-query"`)).Error())
}

func assertAuthNoSecrets(t *testing.T, text string) {
	t.Helper()
	for _, secret := range []string{"private-user", "private-password", "private-query", "private-fragment"} {
		if strings.Contains(text, secret) {
			t.Errorf("secret %q in %s", secret, text)
		}
	}
}

func TestAuthMetadataErrorsAndLogsRedacted(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)
	raw := "https://private-user:private-password@registry.example/a?token=private-query"
	for _, status := range []int{200, 401, 500} {
		client := &http.Client{Transport: authTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("invalid JSON")), Request: r}, nil
		})}
		_, err := resolveVersion(t.Context(), &accessv1.NPM{Registry: raw, Package: "pkg", Version: "1"}, nil, Options{Client: client})
		if err == nil {
			t.Fatal("expected metadata error")
		}
		assertAuthNoSecrets(t, err.Error())
	}
	transportCause := &url.Error{Op: "Get", URL: raw, Err: context.Canceled}
	client := &http.Client{Transport: authTransport(func(*http.Request) (*http.Response, error) { return nil, transportCause })}
	_, err := get(t.Context(), raw, nil, Options{Client: client})
	if err == nil {
		t.Fatal("expected transport error")
	}
	assertAuthNoSecrets(t, err.Error())
	if !errors.Is(err, context.Canceled) {
		t.Fatal("lost underlying error")
	}
	_, _ = getTarball(t.Context(), raw, "https://other.example", &credv1.NPMCredentials{Token: "token"}, Options{Client: client})
	assertAuthNoSecrets(t, logs.String())
}
