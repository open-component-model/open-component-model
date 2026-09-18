package internal

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
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

// streamConfig is the decoded uploader stream block. targetURL is a CEL expression
// (referencing the source resource via the `resource` alias) that resolves to the
// upload URL; method/header/body/noRedirect/mediaType map field-for-field onto the
// resulting Wget access, and queryParams are appended to the resolved URL.
type streamConfig struct {
	Type        runtime.Type        `json:"type"`
	TargetURL   string              `json:"targetURL"`
	QueryParams map[string][]string `json:"queryParams,omitempty"`
	Method      string              `json:"method,omitempty"`
	Header      map[string][]string `json:"header,omitempty"`
	Body        []byte              `json:"body,omitempty"`
	NoRedirect  bool                `json:"noRedirect,omitempty"`
	MediaType   string              `json:"mediaType,omitempty"`
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
// `resource`. It carries the resource identity/labels and the parsed parts of the
// source access URL, since CEL has no url parsing function.
func buildResourceNode(resource descriptorv2.Resource, srcAccess *wgetaccessv1.Wget) (map[string]any, error) {
	parsedSource, err := url.Parse(srcAccess.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid source url %q: %w", srcAccess.URL, err)
	}
	labels := make(map[string]any, len(resource.Labels))
	for _, l := range resource.Labels {
		labels[l.Name] = labelValueString(l.Value)
	}
	extraIdentity := make(map[string]any, len(resource.ExtraIdentity))
	for k, v := range resource.ExtraIdentity {
		extraIdentity[k] = v
	}
	return map[string]any{
		"name":          resource.Name,
		"version":       resource.Version,
		"extraIdentity": extraIdentity,
		"labels":        labels,
		"access": map[string]any{
			"url":       srcAccess.URL,
			"path":      parsedSource.Path,
			"host":      parsedSource.Host,
			"scheme":    parsedSource.Scheme,
			"mediaType": srcAccess.MediaType,
		},
	}, nil
}

// aliasPattern matches the bare `resource` identifier when it is not a member
// access (i.e. not preceded by a dot), so `resource.name` is rewritten but a
// field named `...resource` is not.
var aliasPattern = regexp.MustCompile(`(^|[^.\w])` + resourceAlias + `\b`)

// celTargetURLField turns a user targetURL CEL expression into a standalone
// ${...} field value that the graph runtime evaluates. It rewrites the `resource`
// alias to the concrete environment node path, appends any static query params as
// a literal suffix, and validates the result is a single CEL expression.
func celTargetURLField(rawTargetURL, nodePath string, queryParams map[string][]string) (string, error) {
	trimmed := strings.TrimSpace(rawTargetURL)
	if trimmed == "" {
		return "", fmt.Errorf("stream.targetURL is required")
	}
	rewritten := aliasPattern.ReplaceAllString(trimmed, "${1}"+nodePath)
	if suffix := encodeQuerySuffix(queryParams); suffix != "" {
		rewritten = "(" + rewritten + ") + " + strconvQuote(suffix)
	}
	field := "${" + rewritten + "}"
	if ok, err := celparser.IsStandaloneExpression(field); err != nil || !ok {
		return "", fmt.Errorf("invalid targetURL CEL expression %q: %w", rawTargetURL, err)
	}
	return field, nil
}

// encodeQuerySuffix renders query params as a deterministic "?k=v&..." suffix, or
// an empty string when there are none.
func encodeQuerySuffix(queryParams map[string][]string) string {
	if len(queryParams) == 0 {
		return ""
	}
	values := url.Values{}
	for k, vals := range queryParams {
		for _, v := range vals {
			values.Add(k, v)
		}
	}
	return "?" + values.Encode()
}

// strconvQuote returns a CEL/Go double-quoted string literal for s.
func strconvQuote(s string) string {
	return fmt.Sprintf("%q", s)
}

// targetHostFromExpression best-effort extracts a display host from a raw targetURL
// expression for the transformation label. It parses the leading string literal (if
// any); otherwise returns "target".
func targetHostFromExpression(rawTargetURL string) string {
	trimmed := strings.TrimSpace(rawTargetURL)
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

	srcWget := wgetaccessv1.Wget{}
	if resource.Access == nil {
		return fmt.Errorf("resource access is required")
	}
	if err := wgetaccess.Scheme.Convert(resource.Access, &srcWget); err != nil {
		return fmt.Errorf("uploader requires a wget source access: %w", err)
	}

	resourceID := identityToTransformationID(resource.ToIdentity())
	uploadID := fmt.Sprintf("%sUpload%s", id, resourceID)

	// Inject the source resource as a CEL node the targetURL expression can reference.
	node, err := buildResourceNode(resource, &srcWget)
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
	targetURLField, err := celTargetURLField(cfg.TargetURL, nodePath, cfg.QueryParams)
	if err != nil {
		return err
	}

	mediaType := cfg.MediaType
	if mediaType == "" {
		mediaType = srcWget.MediaType
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
