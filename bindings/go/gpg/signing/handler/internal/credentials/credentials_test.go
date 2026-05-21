package credentials

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
)

func mustEntity(t *testing.T, passphrase string) *openpgp.Entity {
	t.Helper()
	cfg := &packet.Config{RSABits: 2048}
	entity, err := openpgp.NewEntity("test", "", "test@example.com", cfg)
	require.NoError(t, err)
	if passphrase != "" {
		require.NoError(t, entity.EncryptPrivateKeys([]byte(passphrase), cfg))
	}
	return entity
}

func armoredPrivKeyStr(t *testing.T, entity *openpgp.Entity) string {
	t.Helper()
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PrivateKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, entity.SerializePrivateWithoutSigning(w, nil))
	require.NoError(t, w.Close())
	return buf.String()
}

func armoredPubKeyStr(t *testing.T, entity *openpgp.Entity) string {
	t.Helper()
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, entity.Serialize(w))
	require.NoError(t, w.Close())
	return buf.String()
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "pgp-*.asc")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func TestPrivateEntityFromCredentials_Inline(t *testing.T) {
	entity := mustEntity(t, "")
	armored := armoredPrivKeyStr(t, entity)

	got, err := PrivateEntityFromCredentials(&gpgcredentialsv1.GPGCredentials{
		PrivateKeyPGP: armored,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, entity.PrimaryKey.KeyId, got.PrimaryKey.KeyId)
}

func TestPrivateEntityFromCredentials_File(t *testing.T) {
	entity := mustEntity(t, "")
	armored := armoredPrivKeyStr(t, entity)
	path := writeTempFile(t, armored)

	got, err := PrivateEntityFromCredentials(&gpgcredentialsv1.GPGCredentials{
		PrivateKeyPGPFile: path,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, entity.PrimaryKey.KeyId, got.PrimaryKey.KeyId)
}

func TestPrivateEntityFromCredentials_Empty(t *testing.T) {
	got, err := PrivateEntityFromCredentials(&gpgcredentialsv1.GPGCredentials{})
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestPrivateEntityFromCredentials_Passphrase(t *testing.T) {
	const passphrase = "test-credentials-passphrase"
	entity := mustEntity(t, passphrase)
	armored := armoredPrivKeyStr(t, entity)

	// correct passphrase succeeds
	got, err := PrivateEntityFromCredentials(&gpgcredentialsv1.GPGCredentials{
		PrivateKeyPGP: armored,
		Passphrase:    passphrase,
	})
	require.NoError(t, err)
	require.NotNil(t, got)

	// wrong passphrase fails
	_, err = PrivateEntityFromCredentials(&gpgcredentialsv1.GPGCredentials{
		PrivateKeyPGP: armored,
		Passphrase:    "test-wrong-credentials-passphrase",
	})
	require.Error(t, err)
}

func TestPrivateEntityFromCredentials_EncryptedKeyNoPassphrase(t *testing.T) {
	const passphrase = "test-credentials-passphrase"
	entity := mustEntity(t, passphrase)
	armored := armoredPrivKeyStr(t, entity)

	_, err := PrivateEntityFromCredentials(&gpgcredentialsv1.GPGCredentials{
		PrivateKeyPGP: armored,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "passphrase")
}

func TestPublicKeyRingFromCredentials_Inline(t *testing.T) {
	entity := mustEntity(t, "")
	armored := armoredPubKeyStr(t, entity)

	ring, err := PublicKeyRingFromCredentials(&gpgcredentialsv1.GPGCredentials{
		PublicKeyPGP: armored,
	})
	require.NoError(t, err)
	require.Len(t, ring, 1)
	assert.Equal(t, entity.PrimaryKey.KeyId, ring[0].PrimaryKey.KeyId)
}

func TestPublicKeyRingFromCredentials_File(t *testing.T) {
	entity := mustEntity(t, "")
	armored := armoredPubKeyStr(t, entity)
	path := writeTempFile(t, armored)

	ring, err := PublicKeyRingFromCredentials(&gpgcredentialsv1.GPGCredentials{
		PublicKeyPGPFile: path,
	})
	require.NoError(t, err)
	require.Len(t, ring, 1)
}

func TestPublicKeyRingFromCredentials_FallbackToPrivate(t *testing.T) {
	entity := mustEntity(t, "")
	armored := armoredPrivKeyStr(t, entity)

	ring, err := PublicKeyRingFromCredentials(&gpgcredentialsv1.GPGCredentials{
		PrivateKeyPGP: armored,
	})
	require.NoError(t, err)
	require.Len(t, ring, 1)
}

func TestPublicKeyRingFromCredentials_Empty(t *testing.T) {
	ring, err := PublicKeyRingFromCredentials(&gpgcredentialsv1.GPGCredentials{})
	require.NoError(t, err)
	assert.Nil(t, ring)
}

func TestPrivateEntityFromCredentials_EncryptedSubkeyNoPassphrase(t *testing.T) {
	// Build entity with unencrypted primary key but encrypted signing subkey.
	cfg := &packet.Config{RSABits: 2048}
	entity, err := openpgp.NewEntity("test", "", "test@example.com", cfg)
	require.NoError(t, err)

	const passphrase = "test-subkey-passphrase"
	for _, sub := range entity.Subkeys {
		if sub.PrivateKey != nil {
			require.NoError(t, sub.PrivateKey.Encrypt([]byte(passphrase)))
		}
	}

	armored := armoredPrivKeyStr(t, entity)

	_, err = PrivateEntityFromCredentials(&gpgcredentialsv1.GPGCredentials{
		PrivateKeyPGP: armored,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "passphrase")
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
