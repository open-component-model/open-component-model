package spec

import (
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const UploaderConfigType = "uploader.transfer.config.ocm.software"

func init() {
	Scheme.MustRegisterWithAlias(&UploaderConfig{},
		runtime.NewVersionedType(UploaderConfigType, Version),
		runtime.NewUnversionedType(UploaderConfigType),
	)
}

// UploaderConfig is a declarative rule that routes matching resources through a
// custom upload transformer during transfer. It is carried as an entry inside the
// central generic configuration (generic.config.ocm.software/v1), as a sibling of
// [Config], and extracted with [LookupUploaderConfigs]. Each entry is an independent
// rule; entries are not merged.
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: uploader.transfer.config.ocm.software/v1alpha1
//	    match:
//	      accessType: Wget/v1alpha1
//	    stream:
//	      type: HTTPStreaming/v1alpha1
//	      targetURL: 'https://mytarget.registry.com/{{.path}}'
//	      method: PUT
//	      queryParams: {}
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type UploaderConfig struct {
	// +ocm:jsonschema-gen:enum=uploader.transfer.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=uploader.transfer.config.ocm.software
	Type runtime.Type `json:"type"`

	// Match selects the resources this uploader applies to.
	Match UploaderMatch `json:"match"`

	// Stream is the transformer specification used to upload matched resources.
	// It is kept generic so the uploader stays open to different transformer types;
	// the transformer is selected by Stream's own type (e.g. HTTPStreaming/v1alpha1).
	Stream *runtime.Raw `json:"stream"`
}

// UploaderMatch selects resources by their access type and, optionally, their
// identity. A resource matches when its access type matches AccessType and every
// specified identity constraint (Name, ExtraIdentity) also matches. This lets
// multiple uploaders target the same access type while routing different resources
// to different upload targets; the first matching uploader (in declaration order)
// wins, so more specific rules should be declared before broader ones.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type UploaderMatch struct {
	// AccessType is the resource access type this uploader matches (e.g. Wget/v1alpha1).
	AccessType runtime.Type `json:"accessType"`
	// Name optionally restricts the match to resources with this exact name.
	// When empty, resources of any name match.
	Name string `json:"name,omitempty"`
	// ExtraIdentity optionally restricts the match to resources whose identity
	// contains all of these key/value pairs. When empty, no extra-identity
	// constraint is applied.
	ExtraIdentity runtime.Identity `json:"extraIdentity,omitempty"`
}

// Validate rejects a non-matching [UploaderConfig.Type], an empty match access type,
// and a missing or untyped stream. An empty Type is allowed so callers constructing a
// config programmatically (without going through [Scheme.Decode]) do not need to set it.
func (u *UploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	if !u.Type.IsEmpty() {
		if u.Type.Name != UploaderConfigType || (u.Type.Version != "" && u.Type.Version != Version) {
			return fmt.Errorf("invalid type %q (must be %q or %q)",
				u.Type, UploaderConfigType, runtime.NewVersionedType(UploaderConfigType, Version))
		}
	}
	if u.Match.AccessType.IsEmpty() {
		return fmt.Errorf("match.accessType is required")
	}
	if u.Stream == nil || u.Stream.GetType().IsEmpty() {
		return fmt.Errorf("stream is required and must carry a type")
	}
	return nil
}

// LookupUploaderConfigs extracts all uploader configurations from a central generic
// config. All entries of type [UploaderConfigType] are decoded and validated, preserving
// their declaration order. Returns nil if cfg is nil or contains no uploader entries.
func LookupUploaderConfigs(cfg *genericv1.Config) ([]*UploaderConfig, error) {
	filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(UploaderConfigType, Version),
			runtime.NewUnversionedType(UploaderConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter config: %w", err)
	}
	if filtered == nil || len(filtered.Configurations) == 0 {
		return nil, nil
	}
	uploaders := make([]*UploaderConfig, 0, len(filtered.Configurations))
	for _, entry := range filtered.Configurations {
		var u UploaderConfig
		if err := Scheme.Convert(entry, &u); err != nil {
			return nil, fmt.Errorf("failed to decode uploader config: %w", err)
		}
		if err := u.Validate(); err != nil {
			return nil, fmt.Errorf("invalid uploader config: %w", err)
		}
		uploaders = append(uploaders, &u)
	}
	return uploaders, nil
}
