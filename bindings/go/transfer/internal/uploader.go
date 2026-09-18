package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"text/template"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// streamConfig is the decoded uploader stream block. Its fields map field-for-field
// onto the Wget access type: targetURL+queryParams → Wget.URL, method → Wget.Verb,
// and header/body/noRedirect/mediaType pass straight through to the same-named fields.
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

// matchUploader returns the first uploader whose match access type matches accessType
// by name (and version, when the uploader specifies one). Returns nil if none match.
func matchUploader(uploaders []*transferv1alpha1.UploaderConfig, accessType runtime.Type) *transferv1alpha1.UploaderConfig {
	for _, u := range uploaders {
		if u == nil {
			continue
		}
		match := u.Match.AccessType
		if match.Name != accessType.Name {
			continue
		}
		if match.Version != "" && match.Version != accessType.Version {
			continue
		}
		return u
	}
	return nil
}

// resolveTargetURL evaluates the target URL template against the source access URL and
// resource identity, then appends the query params. It is the only build-time computation;
// a missing template variable fails the build rather than emitting a partial URL.
func resolveTargetURL(rawTargetURL string, queryParams map[string][]string, srcAccess *wgetaccessv1.Wget, resource descriptorv2.Resource) (string, error) {
	parsedSource, err := url.Parse(srcAccess.URL)
	if err != nil {
		return "", fmt.Errorf("invalid source url %q: %w", srcAccess.URL, err)
	}
	data := map[string]string{
		"path":    parsedSource.Path,
		"host":    parsedSource.Host,
		"scheme":  parsedSource.Scheme,
		"name":    resource.Name,
		"version": resource.Version,
	}

	tmpl, err := template.New("targetURL").Option("missingkey=error").Parse(rawTargetURL)
	if err != nil {
		return "", fmt.Errorf("invalid targetURL template %q: %w", rawTargetURL, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed evaluating targetURL template %q: %w", rawTargetURL, err)
	}

	resolved, err := url.Parse(buf.String())
	if err != nil {
		return "", fmt.Errorf("resolved targetURL %q is invalid: %w", buf.String(), err)
	}
	if len(queryParams) > 0 {
		q := resolved.Query()
		for k, vals := range queryParams {
			for _, v := range vals {
				q.Add(k, v)
			}
		}
		resolved.RawQuery = q.Encode()
	}
	return resolved.String(), nil
}

// targetHost extracts the host from an absolute URL for display in labels,
// falling back to the full URL when it cannot be parsed.
func targetHost(targetURL string) string {
	if parsed, err := url.Parse(targetURL); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return targetURL
}

// processUploader emits a single HTTPStreaming (or other stream-typed) transformation for
// resource. The target resource is a copy of the source carrying a Wget access built from
// the uploader's stream config, so the transfer plan is literal and deterministic.
func processUploader(resource descriptorv2.Resource, u *transferv1alpha1.UploaderConfig, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, i int) error {
	var cfg streamConfig
	if err := json.Unmarshal(u.Stream.Data, &cfg); err != nil {
		return fmt.Errorf("cannot decode uploader stream config: %w", err)
	}

	srcWget := wgetaccessv1.Wget{}
	if resource.Access == nil {
		return fmt.Errorf("resource access is required")
	}
	if err := wgetaccess.Scheme.Convert(resource.Access, &srcWget); err != nil {
		return fmt.Errorf("uploader requires a wget source access: %w", err)
	}

	targetURL, err := resolveTargetURL(cfg.TargetURL, cfg.QueryParams, &srcWget, resource)
	if err != nil {
		return err
	}

	mediaType := cfg.MediaType
	if mediaType == "" {
		mediaType = srcWget.MediaType
	}
	targetAccess := &wgetaccessv1.Wget{
		Type:       wgetaccess.V1VersionedType,
		URL:        targetURL,
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

	resourceID := identityToTransformationID(resource.ToIdentity())
	uploadID := fmt.Sprintf("%sUpload%s", id, resourceID)

	spec, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource":       resource,
		"targetResource": targetResource,
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for uploader transformation: %w", err)
	}

	label := uploaderLabel(&val.Descriptor.Component, resource.Name, targetHost(targetURL))
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
