package oidcflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	grantTypeTokenExchange  = "urn:ietf:params:oauth:grant-type:token-exchange" //nolint:gosec // RFC 8693 URN, not a credential
	tokenTypeJWT            = "urn:ietf:params:oauth:token-type:jwt"            //nolint:gosec // RFC 8693 URN, not a credential
	tokenTypeIDToken        = "urn:ietf:params:oauth:token-type:id_token"       //nolint:gosec // RFC 8693 URN, not a credential
	DefaultSubjectTokenType = tokenTypeJWT
	DefaultAudience         = "sigstore"

	defaultHTTPTimeout = 2 * time.Minute
	maxErrorBodyBytes  = 512
)

// ExchangeOptions configures an RFC 8693 token exchange request.
type ExchangeOptions struct {
	TokenURL         string
	SubjectToken     string
	SubjectTokenType string
	Audience         string
	HTTPClient       *http.Client
}

// ExchangeToken performs an RFC 8693 token exchange, trading a subject token
// (e.g. a CI/CD machine token) for an OIDC ID token suitable for Sigstore signing.
func ExchangeToken(ctx context.Context, opts ExchangeOptions) (*Token, error) {
	if opts.TokenURL == "" {
		return nil, fmt.Errorf("unable to perform token exchange: TokenURL is required")
	}
	if opts.SubjectToken == "" {
		return nil, fmt.Errorf("unable to perform token exchange: SubjectToken is required")
	}
	if opts.SubjectTokenType == "" {
		opts.SubjectTokenType = DefaultSubjectTokenType
	}
	if opts.Audience == "" {
		opts.Audience = DefaultAudience
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}

	form := url.Values{
		"grant_type":           {grantTypeTokenExchange},
		"subject_token":        {opts.SubjectToken},
		"subject_token_type":   {opts.SubjectTokenType},
		"audience":             {opts.Audience},
		"requested_token_type": {tokenTypeIDToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("unable to create token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to perform token exchange: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("unable to read token exchange response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		excerpt := string(body)
		if len(excerpt) > maxErrorBodyBytes {
			excerpt = excerpt[:maxErrorBodyBytes]
		}
		return nil, fmt.Errorf("token exchange failed (HTTP %d): %s", resp.StatusCode, excerpt)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("unable to parse token exchange response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("unable to resolve identity token: token exchange response missing access_token")
	}

	return &Token{RawToken: tokenResp.AccessToken}, nil
}
