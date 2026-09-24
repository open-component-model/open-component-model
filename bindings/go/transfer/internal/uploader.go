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
	"ocm.software/open-component-model/bindings/go/transform/graph/runtime/resolver"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	wgettransformv1alpha1 "ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

// resourceAlias is the identifier an uploader's targetURL CEL expression uses to
// reference the source resource. processHTTPUploader rewrites it to the concrete
// environment node path before the graph runtime evaluates the expression.
const resourceAlias = "resource"

// resourceNodePath returns the CEL path the `resource` alias is rewritten to. Instead
// of injecting a second copy of the resource into the environment, it points at the
// resource already present in the descriptor environment node (keyed by baseID, see
// addDescriptorToEnvironment) by its index in component.resources. The index is the
// resource's position in the descriptor the environment node was built from, so it is
// exact and stable for the duration of the graph build.
//
// Index selection avoids a CEL filter predicate over the whole resource list: the
// environment node's element type is inferred from the concrete JSON, so an optional
// field such as extraIdentity (omitempty) is absent from the inferred type whenever any
// resource lacks it, and a filter predicate that reads r.extraIdentity then fails type
// checking with "undefined field 'extraIdentity'". Selecting by index never references
// a field that a sibling resource omits.
//
// Every v2 resource field is addressable by appending to the returned path:
// <path>.access.url, <path>.digest.value, <path>.extraIdentity.<key>, <path>.labels,
// and so on.
func resourceNodePath(baseID string, index int) string {
	return fmt.Sprintf("environment.%s.component.resources[%d]", baseID, index)
}

// mediaTypeFromAccess extracts the source access media type (if any) from the resource
// access, used as the default target media type. Returns "" when absent.
func mediaTypeFromAccess(resource descriptorv2.Resource) string {
	if resource.Access == nil || len(resource.Access.Data) == 0 {
		return ""
	}
	var access struct {
		MediaType string `json:"mediaType"`
	}
	if err := json.Unmarshal(resource.Access.Data, &access); err != nil {
		return ""
	}
	return access.MediaType
}

// templateExpressions rewrites the `resource` alias in every ${...} expression across
// the entire JSON object held by raw, in place. It does not hardcode which fields may
// carry expressions: it reuses the graph's own expression pipeline — [celparser.ParseSchemaless]
// discovers every expression field (standalone ${expr} and embedded "pre-${expr}"
// templates alike), each expression's `resource` alias is rewritten to reference
// nodePath, and [resolver.Resolver.UpsertValueAtPath] splices the result back at the
// field's path. Strings without ${...} carry no expressions and pass through unchanged.
func templateExpressions(raw *runtime.Raw, nodePath string) error {
	var obj map[string]any
	if err := json.Unmarshal(raw.Data, &obj); err != nil {
		return fmt.Errorf("cannot decode target access: %w", err)
	}

	fields, err := celparser.ParseSchemaless(obj)
	if err != nil {
		return fmt.Errorf("cannot parse target access expressions: %w", err)
	}

	res := resolver.NewResolver(obj, nil, nil)
	for _, field := range fields {
		current, err := res.GetValueFromPath(field.Path)
		if err != nil {
			return fmt.Errorf("cannot read field %s: %w", field.Path, err)
		}
		value, ok := current.(string)
		if !ok {
			continue
		}
		// Rewrite the alias inside each discovered expression, then substitute it back
		// into the field value. For a standalone ${expr} this replaces the whole value;
		// for an embedded template it rewrites each ${expr} in place.
		rewritten := value
		for _, expr := range field.Expressions {
			original := "${" + expr.Value + "}"
			replaced := "${" + celparser.RewriteIdentifier(expr.Value, resourceAlias, nodePath) + "}"
			rewritten = strings.ReplaceAll(rewritten, original, replaced)
		}
		if err := res.UpsertValueAtPath(field.Path, rewritten); err != nil {
			return fmt.Errorf("cannot rewrite field %s: %w", field.Path, err)
		}
	}

	data, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("cannot re-encode target access: %w", err)
	}
	raw.Data = data
	return nil
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

// processHTTPUploader emits a single HTTPStreaming transformation for resource from an
// [transferv1alpha1.HTTPUploaderConfig]. It builds the target Wget access from the
// config's request fields and templates the whole object (see templateExpressions):
// every ${...} string has its `resource` alias rewritten to the resource's path inside
// the shared descriptor environment node (see resourceNodePath), so no second copy of
// the resource is injected; strings without ${...} are literals and pass through.
func processHTTPUploader(resource descriptorv2.Resource, u *transferv1alpha1.HTTPUploaderConfig, baseID, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, i int) error {
	if u.TargetURL == "" {
		return fmt.Errorf("uploader targetURL is required")
	}
	if resource.Access == nil {
		return fmt.Errorf("resource access is required")
	}

	resourceID := identityToTransformationID(resource.ToIdentity())
	uploadID := fmt.Sprintf("%sUpload%s", id, resourceID)

	// The target media type defaults to the uploader's explicit value, then to the
	// source access media type when it exposes one (wget, OCI, ...).
	mediaType := u.MediaType
	if mediaType == "" {
		mediaType = mediaTypeFromAccess(resource)
	}
	// Build the target access from the raw user strings; expression templating is applied
	// generically to the whole object below rather than to hand-picked fields.
	targetAccess := &wgetaccessv1.Wget{
		Type:       wgetaccess.V1VersionedType,
		URL:        u.TargetURL,
		Verb:       u.Method,
		Header:     u.Header,
		NoRedirect: u.NoRedirect,
		MediaType:  mediaType,
	}
	targetAccessRaw := &runtime.Raw{}
	if err := wgetaccess.Scheme.Convert(targetAccess, targetAccessRaw); err != nil {
		return fmt.Errorf("cannot convert target wget access: %w", err)
	}

	// Rewrite the `resource` alias in every ${...} string across the entire target
	// access object, rather than templating hand-picked fields. Point it at the resource
	// already present in the descriptor environment node (selected by index, see
	// resourceNodePath) so no second copy of the resource is injected; every access field
	// stays addressable generically under resource.access.<field>, so an uploader works
	// with any source access type, not only wget.
	nodePath := resourceNodePath(baseID, i)
	if err := templateExpressions(targetAccessRaw, nodePath); err != nil {
		return fmt.Errorf("cannot template uploader target access: %w", err)
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

	label := uploaderLabel(&val.Descriptor.Component, resource.Name, targetHostFromExpression(u.TargetURL))
	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  wgettransformv1alpha1.HTTPStreamingV1alpha1,
			ID:    uploadID,
			Label: label,
		},
		Spec: spec,
	})
	resourceTransformIDs[i] = uploadID
	return nil
}
