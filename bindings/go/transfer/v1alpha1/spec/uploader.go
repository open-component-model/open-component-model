package spec

import (
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// HTTPUploaderConfigType is the config type that routes matching resources through
// the HTTP streaming upload transformer. The config type itself selects the target
// transformer, so there is no nested transformer sub-type to specify.
const HTTPUploaderConfigType = "http.uploader.transfer.config.ocm.software"

func init() {
	Scheme.MustRegisterWithAlias(&HTTPUploaderConfig{},
		runtime.NewVersionedType(HTTPUploaderConfigType, Version),
		runtime.NewUnversionedType(HTTPUploaderConfigType),
	)
}

// HTTPUploaderConfig is a declarative rule that streams matching resources to a
// custom HTTP target during transfer. It is carried as an entry inside the central
// generic configuration (generic.config.ocm.software/v1), as a sibling of [Config],
// and extracted with [LookupHTTPUploaderConfigs]. Each entry is an independent rule;
// entries are not merged. The config type dedicates it to the HTTP streaming target,
// so the upload request fields are declared inline rather than in a nested stream
// sub-type.
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: http.uploader.transfer.config.ocm.software/v1alpha1
//	    match:
//	      accessType: Wget/v1
//	    targetURL: '${"https://mytarget.registry.com/uploads" + url(resource.access.url).path}'
//	    method: PUT
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type HTTPUploaderConfig struct {
	// +ocm:jsonschema-gen:enum=http.uploader.transfer.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=http.uploader.transfer.config.ocm.software
	Type runtime.Type `json:"type"`

	// Match selects the resources this uploader applies to.
	Match UploaderMatch `json:"match"`

	// TargetURL is a standalone CEL expression wrapped in ${...} (referencing the
	// source resource via the `resource` alias) that resolves to the upload URL.
	// Append any static query string directly inside the expression.
	TargetURL string `json:"targetURL"`
	// Method is the HTTP verb used for the upload request (e.g. PUT).
	Method string `json:"method,omitempty"`
	// Header carries additional HTTP request headers.
	Header map[string][]string `json:"header,omitempty"`
	// Body is an optional static request body.
	Body []byte `json:"body,omitempty"`
	// NoRedirect disables following HTTP redirects for the upload request.
	NoRedirect bool `json:"noRedirect,omitempty"`
	// MediaType overrides the media type recorded on the uploaded resource; when
	// empty it defaults to the source access media type where available.
	MediaType string `json:"mediaType,omitempty"`
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
	// AccessType is the resource access type this uploader matches (e.g. Wget/v1).
	AccessType runtime.Type `json:"accessType"`
	// Name optionally restricts the match to resources with this exact name.
	// When empty, resources of any name match.
	Name string `json:"name,omitempty"`
	// ExtraIdentity optionally restricts the match to resources whose identity
	// contains all of these key/value pairs. When empty, no extra-identity
	// constraint is applied.
	ExtraIdentity runtime.Identity `json:"extraIdentity,omitempty"`
}

// Validate rejects a non-matching [HTTPUploaderConfig.Type], an empty match access
// type, and an empty targetURL. An empty Type is allowed so callers constructing a
// config programmatically (without going through [Scheme.Decode]) do not need to set
// it.
func (u *HTTPUploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	if !u.Type.IsEmpty() {
		if u.Type.Name != HTTPUploaderConfigType || (u.Type.Version != "" && u.Type.Version != Version) {
			return fmt.Errorf("invalid type %q (must be %q or %q)",
				u.Type, HTTPUploaderConfigType, runtime.NewVersionedType(HTTPUploaderConfigType, Version))
		}
	}
	if u.Match.AccessType.IsEmpty() {
		return fmt.Errorf("match.accessType is required")
	}
	if u.TargetURL == "" {
		return fmt.Errorf("targetURL is required")
	}
	return nil
}

// LookupHTTPUploaderConfigs extracts all HTTP uploader configurations from a central
// generic config. All entries of type [HTTPUploaderConfigType] are decoded and
// validated, preserving their declaration order. Returns nil if cfg is nil or
// contains no HTTP uploader entries.
func LookupHTTPUploaderConfigs(cfg *genericv1.Config) ([]*HTTPUploaderConfig, error) {
	filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(HTTPUploaderConfigType, Version),
			runtime.NewUnversionedType(HTTPUploaderConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter config: %w", err)
	}
	if filtered == nil || len(filtered.Configurations) == 0 {
		return nil, nil
	}
	uploaders := make([]*HTTPUploaderConfig, 0, len(filtered.Configurations))
	for _, entry := range filtered.Configurations {
		var u HTTPUploaderConfig
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
