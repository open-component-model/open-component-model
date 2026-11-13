package v2

import (
	"encoding/json"
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// LocalBlobAccessType is the access type of blob local to a component.
const (
	LocalBlobAccessType       = "LocalBlob"
	LegacyLocalBlobAccessType = "localBlob"
	// LegacyLocalOciBlobAccessType is the access type OCM v1 wrote for a local blob in an
	// OCI or CTF repository before it settled on localBlob. It carries the repository local
	// blob reference in "digest" and records no media type, see [LocalBlob.UnmarshalJSON].
	LegacyLocalOciBlobAccessType = "localOciBlob"
	LocalBlobAccessTypeVersion   = "v1"
)

// LocalBlob describes the access for a local blob.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type LocalBlob struct {
	// +ocm:jsonschema-gen:enum=LocalBlob/v1
	// +ocm:jsonschema-gen:enum:deprecated=localBlob/v1,LocalBlob,localBlob,localOciBlob/v1,localOciBlob
	Type runtime.Type `json:"type"`
	// LocalReference is the repository local identity of the blob.
	// it is used by the repository implementation to get access
	// to the blob and if therefore specific to a dedicated repository type.
	LocalReference string `json:"localReference"`
	// MediaType is the media type of the object represented by the blob
	MediaType string `json:"mediaType"`
	// GlobalAccess is an optional field describing a possibility
	// for a global access. If given, it MUST describe a global access method.
	GlobalAccess *runtime.Raw `json:"globalAccess,omitempty"`
	// ReferenceName is an optional static name the object should be
	// use in a local repository context. It is use by a repository
	// to optionally determine a globally referenceable access according
	// to the OCI distribution spec. The result will be stored
	// by the repository in the field ImageReference.
	// The value is typically an OCI repository name optionally
	// followed by a colon ':' and a tag
	ReferenceName string `json:"referenceName,omitempty"`
}

// UnmarshalJSON implements the json.Unmarshaler interface.
//
// A [LegacyLocalOciBlobAccessType] access is decoded as a LocalBlob: OCM v1 wrote the
// repository local blob reference in "digest" there and no media type at all, so without
// the mapping LocalReference would stay empty and the blob could not be resolved.
//
// Its type is normalised to [LocalBlobAccessType], unlike the [LegacyLocalBlobAccessType]
// alias, which keeps its spelling: localOciBlob declares a different field set, so writing
// that type together with a localReference would produce an access no reader can resolve.
func (a *LocalBlob) UnmarshalJSON(data []byte) error {
	// localBlob sheds the methods of LocalBlob so json does not call back into this one.
	type localBlob LocalBlob
	var decoded localBlob
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("decoding local blob access failed: %w", err)
	}
	*a = LocalBlob(decoded)

	if a.Type.Name != LegacyLocalOciBlobAccessType || a.LocalReference != "" {
		return nil
	}

	var legacy struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return fmt.Errorf("decoding legacy %s access failed: %w", LegacyLocalOciBlobAccessType, err)
	}
	a.LocalReference = legacy.Digest
	a.Type = runtime.NewVersionedType(LocalBlobAccessType, LocalBlobAccessTypeVersion)

	return nil
}
