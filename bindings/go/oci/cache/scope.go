package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	credv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
)

// ScopeKey returns a stable, filesystem-safe string that uniquely
// identifies the (registry identity, credential) pair. It is used to
// derive an isolated cache subdirectory so that blobs and manifests
// fetched under one credential set cannot be served to a caller
// holding different credentials.
//
// The key is a 16-hex-character SHA-256 prefix of:
//
//	"<hostname>[:<port>]<path>|<credential-discriminator>"
//
// The credential discriminator is the username (basic auth), or the
// first 16 bytes of the access or refresh token (bearer auth). When
// no credentials are provided the discriminator is empty, producing a
// stable "anonymous" scope that maps to a predictable subdirectory
// rather than mixing with any authenticated scope.
//
// Only the hash, never the raw credential material, is written to
// disk or used as a directory name.
func ScopeKey(identity *identityv1.OCIRegistryIdentity, creds *credv1.OCICredentials) string {
	var b strings.Builder

	if identity != nil {
		b.WriteString(identity.Hostname)
		if identity.Port != "" {
			b.WriteByte(':')
			b.WriteString(identity.Port)
		}
		b.WriteString(identity.Path)
	}

	b.WriteByte('|')

	if creds != nil {
		switch {
		case creds.Username != "":
			b.WriteString(creds.Username)
		case creds.AccessToken != "":
			b.WriteString(creds.AccessToken[:min(len(creds.AccessToken), 16)])
		case creds.RefreshToken != "":
			b.WriteString(creds.RefreshToken[:min(len(creds.RefreshToken), 16)])
		}
	}

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}
