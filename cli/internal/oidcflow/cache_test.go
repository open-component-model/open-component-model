package oidcflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func overrideCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := userCacheDir
	userCacheDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userCacheDir = orig })
	return dir
}

func TestCache(t *testing.T) {
	t.Parallel()

	t.Run("PersistAndLoad", func(t *testing.T) {
		r := require.New(t)
		overrideCacheDir(t)

		tok := &oauth2.Token{
			AccessToken:  "access",
			RefreshToken: "refresh",
			TokenType:    "Bearer",
		}

		err := persistCachedToken("https://issuer.example.com", "my-client", tok, "id-token-value")
		r.NoError(err)

		ct, err := loadCachedToken("https://issuer.example.com", "my-client")
		r.NoError(err)
		r.Equal("access", ct.AccessToken)
		r.Equal("refresh", ct.RefreshToken)
		r.Equal("Bearer", ct.TokenType)
		r.Equal("id-token-value", ct.IDToken)
	})

	t.Run("LoadMissingFile", func(t *testing.T) {
		r := require.New(t)
		overrideCacheDir(t)

		_, err := loadCachedToken("https://issuer.example.com", "no-such-client")
		r.Error(err)
		r.Contains(err.Error(), "read cache file")
	})

	t.Run("LoadCorruptFile", func(t *testing.T) {
		r := require.New(t)
		dir := overrideCacheDir(t)

		h := sha256.Sum256([]byte("https://issuer.example.com" + "\x00" + "corrupt-client"))
		path := filepath.Join(dir, "ocm", "oidc", hex.EncodeToString(h[:])+".json")
		r.NoError(os.MkdirAll(filepath.Dir(path), 0o700))
		r.NoError(os.WriteFile(path, []byte("not json{{{"), 0o600))

		_, err := loadCachedToken("https://issuer.example.com", "corrupt-client")
		r.Error(err)
		r.Contains(err.Error(), "unmarshal cache file")
	})

	t.Run("LoadNoRefreshToken", func(t *testing.T) {
		r := require.New(t)
		dir := overrideCacheDir(t)

		h := sha256.Sum256([]byte("https://issuer.example.com" + "\x00" + "no-refresh"))
		path := filepath.Join(dir, "ocm", "oidc", hex.EncodeToString(h[:])+".json")
		r.NoError(os.MkdirAll(filepath.Dir(path), 0o700))

		ct := cachedToken{AccessToken: "access", TokenType: "Bearer"}
		data, _ := json.Marshal(ct)
		r.NoError(os.WriteFile(path, data, 0o600))

		_, err := loadCachedToken("https://issuer.example.com", "no-refresh")
		r.Error(err)
		r.Contains(err.Error(), "no refresh token")
	})

	t.Run("FilePermissions", func(t *testing.T) {
		r := require.New(t)
		dir := overrideCacheDir(t)

		tok := &oauth2.Token{
			AccessToken:  "access",
			RefreshToken: "refresh",
			TokenType:    "Bearer",
		}

		err := persistCachedToken("https://issuer.example.com", "perm-client", tok, "")
		r.NoError(err)

		h := sha256.Sum256([]byte("https://issuer.example.com" + "\x00" + "perm-client"))
		path := filepath.Join(dir, "ocm", "oidc", hex.EncodeToString(h[:])+".json")

		info, err := os.Stat(path)
		r.NoError(err)
		r.Equal(os.FileMode(0o600), info.Mode().Perm())
	})

	t.Run("RemoveCacheFile", func(t *testing.T) {
		r := require.New(t)
		dir := overrideCacheDir(t)

		tok := &oauth2.Token{
			AccessToken:  "access",
			RefreshToken: "refresh",
			TokenType:    "Bearer",
		}

		err := persistCachedToken("https://issuer.example.com", "remove-client", tok, "")
		r.NoError(err)

		h := sha256.Sum256([]byte("https://issuer.example.com" + "\x00" + "remove-client"))
		path := filepath.Join(dir, "ocm", "oidc", hex.EncodeToString(h[:])+".json")

		_, err = os.Stat(path)
		r.NoError(err)

		removeCacheFile("https://issuer.example.com", "remove-client")

		_, err = os.Stat(path)
		r.True(os.IsNotExist(err))
	})

	t.Run("FilePath", func(t *testing.T) {
		r := require.New(t)
		overrideCacheDir(t)

		path, err := cacheFilePath("https://issuer.example.com", "my-client")
		r.NoError(err)

		h := sha256.Sum256([]byte("https://issuer.example.com" + "\x00" + "my-client"))
		expected := hex.EncodeToString(h[:]) + ".json"
		r.Equal(expected, filepath.Base(path))
		r.Contains(path, filepath.Join("ocm", "oidc"))
	})
}
