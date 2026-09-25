package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
)

// RepositoryKey returns a stable, filesystem-safe string that uniquely
// identifies a registry repository by its location only, without any
// credential material. It is used to derive an isolated cache
// subdirectory per repository so blobs and manifests from different
// repositories do not collide.
//
// The key is a 16-hex-character SHA-256 prefix of:
//
//	"<hostname>[:<port>]<path>"
//
// Credential material is deliberately excluded: short-lived tokens
// would create new scope keys and kill cache reuse. Use
// [RemotePolicyAlways] instead when remote authorisation must be
// verified on every cache hit.
func RepositoryKey(identity *identityv1.OCIRegistryIdentity) string {
	var b strings.Builder
	if identity != nil {
		b.WriteString(identity.Hostname)
		if identity.Port != "" {
			b.WriteByte(':')
			b.WriteString(identity.Port)
		}
		b.WriteString(identity.Path)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}
