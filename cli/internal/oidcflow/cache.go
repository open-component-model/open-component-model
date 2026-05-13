package oidcflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type cachedToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token"`
}

// userCacheDir is the function used to determine the base cache directory.
// Tests override this to use t.TempDir().
var userCacheDir = os.UserCacheDir

func cacheFilePath(issuer, clientID string) (string, error) {
	cacheDir, err := userCacheDir()
	if err != nil {
		return "", fmt.Errorf("determine user cache directory: %w", err)
	}
	h := sha256.Sum256([]byte(issuer + "\x00" + clientID))
	return filepath.Join(cacheDir, "ocm", "oidc", hex.EncodeToString(h[:])+".json"), nil
}

func persistCachedToken(issuer, clientID string, tok *oauth2.Token, idToken string) error {
	path, err := cacheFilePath(issuer, clientID)
	if err != nil {
		return err
	}

	ct := cachedToken{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		TokenType:    tok.TokenType,
		IDToken:      idToken,
	}

	data, err := json.Marshal(ct)
	if err != nil {
		return fmt.Errorf("marshal cached token: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write cache file: %w", err)
	}

	return nil
}

func loadCachedToken(issuer, clientID string) (*cachedToken, error) {
	path, err := cacheFilePath(issuer, clientID)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cache file: %w", err)
	}

	var ct cachedToken
	if err := json.Unmarshal(data, &ct); err != nil {
		return nil, fmt.Errorf("unmarshal cache file: %w", err)
	}

	if ct.RefreshToken == "" {
		return nil, fmt.Errorf("cached token has no refresh token")
	}

	return &ct, nil
}

func refreshCachedToken(ctx context.Context, issuer string, provider *oidc.Provider, cfg *oauth2.Config, ct *cachedToken) (*Token, error) {
	tok := &oauth2.Token{
		AccessToken:  ct.AccessToken,
		RefreshToken: ct.RefreshToken,
		TokenType:    ct.TokenType,
	}

	src := cfg.TokenSource(ctx, tok)
	newTok, err := src.Token()
	if err != nil {
		removeCacheFile(issuer, cfg.ClientID)
		return nil, fmt.Errorf("refresh token: %w", err)
	}

	// Prefer id_token from refresh response; fall back to cached one.
	// Dex does not include id_token in refresh responses.
	rawIDToken, _ := newTok.Extra("id_token").(string)
	if rawIDToken == "" {
		rawIDToken = ct.IDToken
	}
	if rawIDToken == "" {
		removeCacheFile(issuer, cfg.ClientID)
		return nil, fmt.Errorf("no id_token available (not in refresh response or cache)")
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	if _, err := verifier.Verify(ctx, rawIDToken); err != nil {
		removeCacheFile(issuer, cfg.ClientID)
		return nil, fmt.Errorf("verify id_token: %w", err)
	}

	if err := persistCachedToken(issuer, cfg.ClientID, newTok, rawIDToken); err != nil {
		removeCacheFile(issuer, cfg.ClientID)
		return nil, fmt.Errorf("persist refreshed token: %w", err)
	}

	return &Token{RawToken: rawIDToken}, nil
}

func removeCacheFile(issuer, clientID string) {
	path, err := cacheFilePath(issuer, clientID)
	if err != nil {
		return
	}
	_ = os.Remove(path)
}
