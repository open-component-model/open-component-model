package credentials

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
)

func TestPublicKeyBytes(t *testing.T) {
	missingFile := filepath.Join(t.TempDir(), "nonexistent.asc")

	tests := []struct {
		name    string
		creds   *gpgcredentialsv1.GPGCredentials
		want    []byte
		wantErr string
	}{
		{name: "nil credentials"},
		{name: "public key preferred over private key", creds: &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: "pub", PrivateKeyPGP: "priv"}, want: []byte("pub")},
		{name: "falls back to private key", creds: &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: "priv"}, want: []byte("priv")},
		{name: "unreadable public key file", creds: &gpgcredentialsv1.GPGCredentials{PublicKeyPGPFile: missingFile, PrivateKeyPGP: "priv"}, wantErr: "load public key: "},
		{name: "unreadable private key fallback", creds: &gpgcredentialsv1.GPGCredentials{PrivateKeyPGPFile: missingFile}, wantErr: "load private key as fallback for verification: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			b, err := PublicKeyBytes(tt.creds)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tt.want, b)
		})
	}
}

func TestLoadBytes(t *testing.T) {
	existingFile := filepath.Join(t.TempDir(), "data.asc")
	require.NoError(t, os.WriteFile(existingFile, []byte("from-file"), 0o600))
	missingFile := filepath.Join(t.TempDir(), "nonexistent.asc")

	tests := []struct {
		name    string
		val     string
		file    string
		want    []byte
		wantErr bool
	}{
		{name: "inline value returned directly", val: "hello", want: []byte("hello")},
		{name: "file read when inline empty", file: existingFile, want: []byte("from-file")},
		{name: "both empty returns nil"},
		{name: "missing file returns error", file: missingFile, wantErr: true},
		{name: "inline takes priority over file", val: "inline", file: existingFile, want: []byte("inline")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := loadBytes(tt.val, tt.file)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, b)
		})
	}
}
