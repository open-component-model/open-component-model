package v1

import (
	"errors"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	S3BucketIdentityType = "S3Bucket"
	Version              = "v1"
)

// Type is the unversioned consumer identity type for S3 objects (backward compat).
var Type = runtime.NewUnversionedType(S3BucketIdentityType)

// VersionedType is the versioned consumer identity type.
var VersionedType = runtime.NewVersionedType(S3BucketIdentityType, Version)

// S3BucketIdentity is the typed consumer identity for objects read from an S3 bucket.
// It always carries the object path (bucket and key), and the URL attributes only for
// an S3-compatible store reached through a custom endpoint — AWS itself is identified
// by the path alone.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type S3BucketIdentity struct {
	// +ocm:jsonschema-gen:enum=S3Bucket/v1
	// +ocm:jsonschema-gen:enum:deprecated=S3Bucket
	Type     runtime.Type `json:"type"`
	Hostname string       `json:"hostname,omitempty"`
	Scheme   string       `json:"scheme,omitempty"`
	Port     string       `json:"port,omitempty"`
	Path     string       `json:"path,omitempty"`
}

// ToIdentity converts an [S3BucketIdentity] into a [runtime.Identity].
// Empty fields are omitted from the resulting map. If the type field is unset,
// the canonical [VersionedType] is used.
func ToIdentity(identity *S3BucketIdentity) runtime.Identity {
	if identity == nil {
		return nil
	}
	id := runtime.Identity{}
	typ := identity.Type
	if typ.IsEmpty() {
		typ = VersionedType
	}
	id.SetType(typ)
	if identity.Hostname != "" {
		id[runtime.IdentityAttributeHostname] = identity.Hostname
	}
	if identity.Scheme != "" {
		id[runtime.IdentityAttributeScheme] = identity.Scheme
	}
	if identity.Port != "" {
		id[runtime.IdentityAttributePort] = identity.Port
	}
	if identity.Path != "" {
		id[runtime.IdentityAttributePath] = identity.Path
	}
	return id
}

// FromIdentity converts a [runtime.Identity] into an [S3BucketIdentity].
// Attributes outside the S3 identity schema are ignored. If the type attribute is
// missing or empty, the canonical [VersionedType] is used.
func FromIdentity(id runtime.Identity) *S3BucketIdentity {
	if id == nil {
		return nil
	}
	out := &S3BucketIdentity{
		Hostname: id[runtime.IdentityAttributeHostname],
		Scheme:   id[runtime.IdentityAttributeScheme],
		Port:     id[runtime.IdentityAttributePort],
		Path:     id[runtime.IdentityAttributePath],
	}

	typ, err := id.ParseType()
	if err != nil {
		slog.Debug("failed to parse identity type, defaulting to versioned type", "error", err)
	}
	if typ.IsEmpty() {
		typ = VersionedType
	}
	out.Type = typ
	return out
}

// IdentityFromObject derives the credential consumer identity for an S3 object from
// its bucket, key and optional endpoint. Without an endpoint the identity carries only
// the object path, because AWS is reached at a well-known host; with one, the endpoint's
// scheme, hostname and port are added so consumer entries can be scoped to that store.
// The unversioned [Type] is used, consistent with the other consumer identities.
//
// An S3 key is an opaque string in which "//", ".", "..", "%", "?" and "#" are
// ordinary characters naming real, distinct objects, so it is carried into the
// identity byte for byte.
func IdentityFromObject(bucketName, objectKey, endpoint string) (runtime.Identity, error) {
	if bucketName == "" {
		return nil, errors.New("bucketName is required")
	}

	loc := bucketName
	if objectKey != "" {
		loc += "/" + objectKey
	}

	identity := runtime.Identity{}
	if endpoint != "" {
		id, err := runtime.ParseURLToIdentity(endpoint)
		if err != nil {
			return nil, fmt.Errorf("error parsing s3 endpoint to identity: %w", err)
		}
		identity = id
	}
	// The endpoint contributes its URL attributes but not its path: the path names the
	// object as bucketName/objectKey, as ocmv1 resolves it, so a consumer entry stays
	// the same wherever the store is reached.
	identity[runtime.IdentityAttributePath] = loc

	identity.SetType(Type)

	return identity, nil
}
