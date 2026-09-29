package spec

import (
	"fmt"
	"slices"
	"strings"

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

// UploaderConfig is the common contract implemented by every uploader configuration
// type. An uploader is a declarative rule that decides what happens to the resources it
// selects during transfer (upload them to a custom target, copy them as local blobs, or
// keep them by reference). Concrete types carry the target-specific fields inline; the
// config type itself selects the handling.
//
// Each uploader selects resources with one CEL boolean expression, its match. The
// expression sees `resource` (the source resource, dynamically typed) and `target` (the
// transfer target as a map with `type`); access types are tested with the alias-aware
// resource.access.isType("OCIImage") or resource.access.isType(["OCIImage", "Helm"]).
//
// All uploader configurations are extracted from the central generic config with
// [LookupUploaderConfigs], which preserves declaration order: the first uploader whose
// match is true handles the resource. A selected uploader that cannot handle the
// resource fails the transfer; there is no fall-through to later uploaders.
type UploaderConfig interface {
	runtime.Typed
	// EffectiveMatch returns the CEL expression that selects resources: the configured
	// match, or the type's default.
	EffectiveMatch() string
	// Validate reports whether the configuration is well-formed.
	Validate() error
}

// TypeResolver lists the aliases of an access type (ociArtifact, OCIImage, OCIImage/v1 are
// aliases of one type), so uploader rules match whichever alias a descriptor uses.
// [runtime.Scheme] implements it.
type TypeResolver interface {
	// ResolveCanonicalType returns the canonical type of typ or one of its aliases.
	ResolveCanonicalType(typ runtime.Type) (canonical runtime.Type, ok bool)
	// AliasesIter yields all aliases of a canonical type.
	AliasesIter(canonical runtime.Type) iter.Seq[runtime.Type]
}

// HTTPUploaderConfig is a declarative rule that streams matching resources to a
// custom HTTP target during transfer. It is carried as an entry inside the central
// generic configuration (generic.config.ocm.software/v1), as a sibling of [Config],
// and extracted with [LookupUploaderConfigs]. Each entry is an independent rule;
// entries are not merged. The config type dedicates it to the HTTP streaming target,
// so the upload request fields are declared inline rather than in a nested stream
// sub-type.
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: http.uploader.transfer.config.ocm.software/v1alpha1
//	    match: resource.access.isType("Wget")
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

	// Match is a CEL boolean expression selecting the resources this uploader streams, e.g.
	// `resource.access.isType("Wget")`. It sees `resource` and `target`; test access types
	// with resource.access.isType. Required: the HTTP uploader has no default.
	Match string `json:"match"`

	// TargetURL is a standalone CEL expression wrapped in ${...} (referencing the
	// source resource via the `resource` alias) that resolves to the upload URL.
	// Append any static query string directly inside the expression.
	TargetURL string `json:"targetURL"`
	// Method is the HTTP verb used for the upload request (e.g. PUT).
	Method string `json:"method,omitempty"`
	// Header carries additional HTTP request headers.
	Header map[string][]string `json:"header,omitempty"`
	// NoRedirect disables following HTTP redirects for the upload request.
	NoRedirect bool `json:"noRedirect,omitempty"`
	// MediaType overrides the media type recorded on the uploaded resource; when
	// empty it defaults to the source access media type where available.
	MediaType string `json:"mediaType,omitempty"`
}

// Validate rejects a non-matching [HTTPUploaderConfig.Type], an empty match, and an
// empty targetURL. An empty Type is allowed so callers constructing a config
// programmatically (without going through [Scheme.Decode]) do not need to set it.
func (u *HTTPUploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	if err := validateUploaderType(u.Type, HTTPUploaderConfigType); err != nil {
		return err
	}
	if strings.TrimSpace(u.Match) == "" {
		return fmt.Errorf("match is required")
	}
	if u.TargetURL == "" {
		return fmt.Errorf("targetURL is required")
	}
	return nil
}

// validateUploaderType rejects a non-empty t that is not configType or
// configType/Version.
func validateUploaderType(t runtime.Type, configType string) error {
	if t.IsEmpty() || (t.Name == configType && (t.Version == "" || t.Version == Version)) {
		return nil
	}
	return fmt.Errorf("invalid type %q (must be %q or %q)", t, configType, runtime.NewVersionedType(configType, Version))
}

// EffectiveMatch returns the configured match; HTTP uploaders have no default. It
// implements [UploaderConfig].
func (u *HTTPUploaderConfig) EffectiveMatch() string {
	if u == nil {
		return ""
	}
	return u.Match
}

// LookupUploaderConfigs extracts all uploader configurations from a central generic
// config. It walks the config entries in declaration order and recognizes an entry as
// an uploader purely by whether its registered type implements [UploaderConfig] — no
// separate registration is required beyond registering the type in [Scheme]. Recognized
// entries are decoded and validated; the preserved order lets the first match win.
// Entries of unrelated types (including [Config]) are ignored. Returns nil if cfg is
// nil or contains no uploader entries.
func LookupUploaderConfigs(cfg *genericv1.Config) ([]UploaderConfig, error) {
	if cfg == nil || len(cfg.Configurations) == 0 {
		return nil, nil
	}
	var uploaders []UploaderConfig
	for _, entry := range cfg.Configurations {
		// An entry is an uploader iff its type is registered in Scheme and the
		// prototype implements UploaderConfig. NewObject fails for types unknown to
		// this Scheme (e.g. transfer/s3/oci config entries), which are not uploaders.
		obj, err := Scheme.NewObject(entry.GetType())
		if err != nil {
			continue
		}
		u, ok := obj.(UploaderConfig)
		if !ok {
			continue
		}
		if err := runtime.DecodeStrict(entry, u); err != nil {
			return nil, fmt.Errorf("failed to decode uploader config: %w", err)
		}
		if err := u.Validate(); err != nil {
			return nil, fmt.Errorf("invalid uploader config: %w", err)
		}
		uploaders = append(uploaders, u)
	}
	return uploaders, nil
}

// uploaderTypeSuffix is the type-name suffix every uploader configuration uses:
// <name>.uploader.transfer.config.ocm.software.
const uploaderTypeSuffix = ".uploader." + ConfigType

// UploaderTypes returns the default (versioned) type of every uploader configuration
// registered in Scheme, sorted by runtime.CompareTypesLexicographically.
func UploaderTypes() []runtime.Type {
	var out []runtime.Type
	for t := range Scheme.GetTypes() {
		obj, err := Scheme.NewObject(t)
		if err != nil {
			continue
		}
		if _, ok := obj.(UploaderConfig); ok {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, runtime.CompareTypesLexicographically)
	return out
}

// UploaderName returns the short name of an uploader type, e.g. "oci".
func UploaderName(t runtime.Type) string {
	return strings.TrimSuffix(t.Name, uploaderTypeSuffix)
}

// ResolveUploaderType resolves a short name ("oci") or a full type
// ("oci.uploader.transfer.config.ocm.software[/v1alpha1]") to a type registered in
// Scheme whose prototype implements UploaderConfig.
func ResolveUploaderType(name string) (runtime.Type, error) {
	var t runtime.Type
	var err error
	if strings.Contains(name, ".") {
		t, err = runtime.TypeFromString(name)
	} else {
		t = runtime.NewVersionedType(name+uploaderTypeSuffix, Version)
	}
	if err == nil {
		if obj, newErr := Scheme.NewObject(t); newErr == nil {
			if _, ok := obj.(UploaderConfig); ok {
				return t, nil
			}
		}
	}
	types := UploaderTypes()
	names := make([]string, 0, len(types))
	for _, ut := range types {
		names = append(names, UploaderName(ut))
	}
	return runtime.Type{}, fmt.Errorf("unknown uploader %q (available: %s)", name, strings.Join(names, ", "))
}
