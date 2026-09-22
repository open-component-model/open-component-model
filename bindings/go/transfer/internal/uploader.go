package internal

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	celparser "ocm.software/open-component-model/bindings/go/cel/expression/parser"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// streamConfig is the decoded uploader stream block. targetURL is a standalone CEL
// expression wrapped in ${...} (referencing the source resource via the `resource`
// alias) that resolves to the upload URL; method/header/body/noRedirect/mediaType
// map field-for-field onto the resulting Wget access. Append any static query
// string directly inside the targetURL expression.
type streamConfig struct {
	Type       runtime.Type        `json:"type"`
	TargetURL  string              `json:"targetURL"`
	Method     string              `json:"method,omitempty"`
	Header     map[string][]string `json:"header,omitempty"`
	Body       []byte              `json:"body,omitempty"`
	NoRedirect bool                `json:"noRedirect,omitempty"`
	MediaType  string              `json:"mediaType,omitempty"`
}

// matchUploader returns the first uploader whose match applies to resource: the
// access type must match by name (and version, when the uploader specifies one),
// the optional Name must equal the resource name, and every optional ExtraIdentity
// entry must be present with an equal value in the resource's identity. Returns nil
// if none match. Declaration order is significant — the first match wins, so more
// specific rules should precede broader ones.
func matchUploader(uploaders []*transferv1alpha1.UploaderConfig, resource descriptorv2.Resource) *transferv1alpha1.UploaderConfig {
	accessType := resource.Access.Type
	identity := resource.ToIdentity()
	for _, u := range uploaders {
		if u == nil {
			continue
		}
		match := u.Match
		if match.AccessType.Name != accessType.Name {
			continue
		}
		if match.AccessType.Version != "" && match.AccessType.Version != accessType.Version {
			continue
		}
		if match.Name != "" && match.Name != resource.Name {
			continue
		}
		if !identityContains(identity, match.ExtraIdentity) {
			continue
		}
		return u
	}
	return nil
}

// identityContains reports whether every key/value pair in want is present with an
// equal value in have. An empty want always matches.
func identityContains(have, want runtime.Identity) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

// labelValueString renders a label's raw JSON value for use in a URL template. A
// JSON string value is unquoted (so "prod" becomes prod); any other JSON value is
// returned as its compact JSON text.
func labelValueString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// resourceAlias is the identifier an uploader's targetURL CEL expression uses to
// reference the source resource. processUploader rewrites it to the concrete
// environment node path before the graph runtime evaluates the expression.
const resourceAlias = "resource"

// uploadsEnvKey is the environment map key under which source-resource nodes are
// injected, addressable in CEL as environment.uploads.<uploadID>.
const uploadsEnvKey = "uploads"

// buildResourceNode builds the CEL node value exposed to a targetURL expression as
// `resource`. It works with any access type: the access is decoded generically from
// its raw JSON so every access field is addressable under resource.access.<field>
// (e.g. resource.access.url for wget, resource.access.imageReference for OCI,
// resource.access.bucket for S3). A targetURL expression that needs the individual
// URL parts (scheme/host/path/...) parses them with the inbuilt url() CEL function,
// e.g. url(resource.access.url).path.
func buildResourceNode(resource descriptorv2.Resource) (map[string]any, error) {
	labels := make(map[string]any, len(resource.Labels))
	for _, l := range resource.Labels {
		labels[l.Name] = labelValueString(l.Value)
	}
	extraIdentity := make(map[string]any, len(resource.ExtraIdentity))
	for k, v := range resource.ExtraIdentity {
		extraIdentity[k] = v
	}

	access, err := decodeAccessFields(resource.Access)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"name":          resource.Name,
		"version":       resource.Version,
		"type":          resource.Type,
		"extraIdentity": extraIdentity,
		"labels":        labels,
		"access":        access,
	}, nil
}

// decodeAccessFields decodes a resource access into a generic field map so an
// uploader's targetURL CEL expression can reference any access field by name.
func decodeAccessFields(access runtime.Typed) (map[string]any, error) {
	if access == nil {
		return nil, fmt.Errorf("resource access is required")
	}
	raw := &runtime.Raw{}
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(access, raw); err != nil {
		return nil, fmt.Errorf("cannot decode resource access: %w", err)
	}
	fields := map[string]any{}
	if len(raw.Data) > 0 {
		if err := json.Unmarshal(raw.Data, &fields); err != nil {
			return nil, fmt.Errorf("cannot decode resource access fields: %w", err)
		}
	}
	return fields, nil
}

// celTargetURLField validates that the user-supplied targetURL is a single
// standalone CEL expression wrapped in ${...} and rewrites the `resource` alias
// to the concrete environment node path. The result is a standalone ${...} field
// value the graph runtime evaluates.
func celTargetURLField(rawTargetURL, nodePath string) (string, error) {
	trimmed := strings.TrimSpace(rawTargetURL)
	if trimmed == "" {
		return "", fmt.Errorf("stream.targetURL is required")
	}
	standalone, err := celparser.IsStandaloneExpression(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid targetURL CEL expression %q: %w", rawTargetURL, err)
	}
	if !standalone {
		return "", fmt.Errorf("stream.targetURL must be a single CEL expression wrapped in ${...}, got %q", rawTargetURL)
	}
	return "${" + rewriteAlias(trimmed[len("${"):len(trimmed)-len("}")], resourceAlias, nodePath) + "}", nil
}

// rewriteAlias replaces the bare identifier alias with replacement everywhere it
// appears as an identifier token in the CEL source expr, leaving occurrences
// inside string literals untouched. An identifier match requires that the
// preceding character is not part of an identifier or a member-access dot (so
// `resource` is rewritten but `myresource` and `x.resource` are not) and that
// the following character does not continue the identifier.
func rewriteAlias(expr, alias, replacement string) string {
	var b strings.Builder
	b.Grow(len(expr))
	var inString byte // 0 when outside a string literal, else the opening quote
	escaped := false
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		if inString != 0 {
			b.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == inString:
				inString = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inString = c
			b.WriteByte(c)
			continue
		}
		if isIdentifierStart(c) && strings.HasPrefix(expr[i:], alias) {
			end := i + len(alias)
			prev := byte(0)
			if i > 0 {
				prev = expr[i-1]
			}
			next := byte(0)
			if end < len(expr) {
				next = expr[end]
			}
			if !isIdentifierPart(prev) && prev != '.' && !isIdentifierPart(next) {
				b.WriteString(replacement)
				i = end - 1
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isIdentifierStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentifierPart(c byte) bool {
	return isIdentifierStart(c) || (c >= '0' && c <= '9')
}

// targetHostFromExpression best-effort extracts a display host from a raw targetURL
// expression for the transformation label. It parses the leading string literal (if
// any); otherwise returns "target".
func targetHostFromExpression(rawTargetURL string) string {
	trimmed := strings.TrimSpace(rawTargetURL)
	// Look past the leading ${ delimiter (and any inner whitespace) to the first
	// token, which is a string literal for the common `"https://host" + ...` form.
	trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "${"))
	for _, quote := range []byte{'"', '\''} {
		if len(trimmed) > 0 && trimmed[0] == quote {
			if end := strings.IndexByte(trimmed[1:], quote); end >= 0 {
				if parsed, err := url.Parse(trimmed[1 : 1+end]); err == nil && parsed.Host != "" {
					return parsed.Host
				}
			}
		}
	}
	return "target"
}

// processUploader emits a single HTTPStreaming (or other stream-typed) transformation
// for resource. It injects the source resource as a CEL node into the graph environment
// (addressable as environment.uploads.<uploadID>) and sets the target Wget access URL to
// a CEL expression derived from the uploader's targetURL, so the graph runtime resolves
// the final URL from the source resource. The rest of the stream config maps onto the
// target Wget access field-for-field.
func processUploader(resource descriptorv2.Resource, u *transferv1alpha1.UploaderConfig, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, i int) error {
	var cfg streamConfig
	if err := json.Unmarshal(u.Stream.Data, &cfg); err != nil {
		return fmt.Errorf("cannot decode uploader stream config: %w", err)
	}
	if strings.TrimSpace(cfg.TargetURL) == "" {
		return fmt.Errorf("uploader stream.targetURL is required")
	}

	if resource.Access == nil {
		return fmt.Errorf("resource access is required")
	}

	resourceID := identityToTransformationID(resource.ToIdentity())
	uploadID := fmt.Sprintf("%sUpload%s", id, resourceID)

	// Inject the source resource as a CEL node the targetURL expression can reference.
	// The node exposes every access field generically under resource.access.<field>,
	// so an uploader works with any source access type, not only wget.
	node, err := buildResourceNode(resource)
	if err != nil {
		return err
	}
	uploads, _ := tgd.Environment.Data[uploadsEnvKey].(map[string]any)
	if uploads == nil {
		uploads = map[string]any{}
		tgd.Environment.Data[uploadsEnvKey] = uploads
	}
	uploads[uploadID] = node

	nodePath := fmt.Sprintf("environment.%s.%s", uploadsEnvKey, uploadID)
	targetURLField, err := celTargetURLField(cfg.TargetURL, nodePath)
	if err != nil {
		return err
	}

	// The target media type defaults to the uploader's explicit value, then to the
	// source access media type when it exposes one (wget, OCI, ...).
	mediaType := cfg.MediaType
	if mediaType == "" {
		if access, ok := node["access"].(map[string]any); ok {
			if mt, ok := access["mediaType"].(string); ok {
				mediaType = mt
			}
		}
	}
	targetAccess := &wgetaccessv1.Wget{
		Type:       wgetaccess.V1VersionedType,
		URL:        targetURLField,
		Verb:       cfg.Method,
		Header:     cfg.Header,
		Body:       cfg.Body,
		NoRedirect: cfg.NoRedirect,
		MediaType:  mediaType,
	}
	targetAccessRaw := &runtime.Raw{}
	if err := wgetaccess.Scheme.Convert(targetAccess, targetAccessRaw); err != nil {
		return fmt.Errorf("cannot convert target wget access: %w", err)
	}

	targetResource := *resource.DeepCopy()
	targetResource.Access = targetAccessRaw

	spec, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource":       resource,
		"targetResource": targetResource,
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for uploader transformation: %w", err)
	}

	label := uploaderLabel(&val.Descriptor.Component, resource.Name, targetHostFromExpression(cfg.TargetURL))
	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  u.Stream.GetType(),
			ID:    uploadID,
			Label: label,
		},
		Spec: spec,
	})
	resourceTransformIDs[i] = uploadID
	return nil
}
