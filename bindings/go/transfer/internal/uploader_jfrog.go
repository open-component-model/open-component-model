package internal

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"ocm.software/open-component-model/bindings/go/blob/compression"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helmtransformer "ocm.software/open-component-model/bindings/go/helm/transformation"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// processJFrogHelmUploader emits a single HTTPStreaming transformation for resource from a
// [transferv1alpha1.JFrogHelmUploaderConfig]: the chart archive (opened by the helm
// [helmtransformer.ChartArchiveOpener]) is PUT to <url>/artifactory/<repository>/<name>-<version>.tgz
// and the resource is published with a Helm/v1 access on <url>/artifactory/api/helm/<repository>.
// Unless disabled, a POST to <url>/artifactory/api/helm/<repository>/reindex follows the upload,
// because Artifactory does not reliably add deployed charts to index.yaml on its own.
//
// Chart name and version default to those of the source chart (see sourceChart) and are CEL
// operands (see celOperand) templated like the HTTP uploader. A local blob source is first
// buffered to a file by a Get*LocalResource transformation; the returned expressions reference
// that file for cleanup.
func processJFrogHelmUploader(resource descriptorv2.Resource, access runtime.Typed, u *transferv1alpha1.JFrogHelmUploaderConfig, baseID, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, i int) ([]string, error) {
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}
	if err := u.Validate(); err != nil {
		return nil, fmt.Errorf("invalid jfrog helm uploader: %w", err)
	}
	base, err := url.Parse(u.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid artifactory url: %w", err)
	}
	uploadBase, err := url.JoinPath(u.URL, "artifactory", u.Repository)
	if err != nil {
		return nil, fmt.Errorf("invalid artifactory url: %w", err)
	}
	helmRepo, err := url.JoinPath(u.URL, "artifactory", "api", "helm", u.Repository)
	if err != nil {
		return nil, fmt.Errorf("invalid artifactory url: %w", err)
	}

	defaultName, defaultVersion := sourceChart(access)
	name := celOperand(u.ChartName, literalOr(defaultName, resourceAlias+".name"))
	version := celOperand(u.ChartVersion, literalOr(defaultVersion, resourceAlias+".version"))
	request := &wgetaccessv1.Wget{
		Type:      wgetaccess.V1VersionedType,
		URL:       fmt.Sprintf(`${%s + %s + "-" + %s + ".tgz"}`, strconv.Quote(uploadBase+"/"), name, version),
		Verb:      http.MethodPut,
		MediaType: compression.MediaTypeGzip,
	}
	published := &helmaccessv1.Helm{
		Type:           runtime.NewVersionedType(helmaccessv1.Type, helmaccessv1.Version),
		HelmRepository: helmRepo,
		HelmChart:      fmt.Sprintf(`${%s + ":" + %s}`, name, version),
	}

	nodePath := resourceNodePath(baseID, i)
	requestRaw, err := templatedAccess(wgetaccess.Scheme, request, nodePath, "request")
	if err != nil {
		return nil, err
	}
	publishedRaw, err := templatedAccess(helmaccess.Scheme, published, nodePath, "published")
	if err != nil {
		return nil, err
	}

	uploadID := fmt.Sprintf("%sUpload%s", id, identityToTransformationID(resource.ToIdentity()))
	extra, fileExprs, err := jfrogHelmExtras(resource, access, u, helmRepo, uploadID, id, val, tgd)
	if err != nil {
		return nil, err
	}
	label := uploaderLabel(&val.Descriptor.Component, resource.Name, base.Host)
	if err := appendHTTPStreaming(tgd, uploadID, label, resource, requestRaw, publishedRaw, extra); err != nil {
		return nil, err
	}
	resourceTransformIDs[i] = uploadID
	return fileExprs, nil
}

// jfrogHelmExtras returns the optional HTTPStreaming spec fields: the chart archive opener,
// the reindex request and, for a local blob, the buffered source file (emitting the
// transformation that buffers it) together with the cleanup expression for that file.
func jfrogHelmExtras(resource descriptorv2.Resource, access runtime.Typed, u *transferv1alpha1.JFrogHelmUploaderConfig, helmRepo, uploadID, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition) (map[string]any, []string, error) {
	extra := map[string]any{"opener": helmtransformer.ChartArchiveOpener}
	if u.ReindexEnabled() {
		reindexURL, err := url.JoinPath(helmRepo, "reindex")
		if err != nil {
			return nil, nil, fmt.Errorf("invalid artifactory url: %w", err)
		}
		reindex := &runtime.Raw{}
		if err := wgetaccess.Scheme.Convert(&wgetaccessv1.Wget{Type: wgetaccess.V1VersionedType, URL: reindexURL, Verb: http.MethodPost}, reindex); err != nil {
			return nil, nil, fmt.Errorf("cannot convert uploader reindex request: %w", err)
		}
		extra["afterUpload"] = reindex
	}
	if _, ok := access.(*descriptorv2.LocalBlob); !ok {
		return extra, nil, nil
	}
	getID, err := appendGetLocalResource(resource, id, val, tgd)
	if err != nil {
		return nil, nil, err
	}
	extra["sourceFile"] = fmt.Sprintf("${%s.output.file}", getID)
	return extra, []string{fmt.Sprintf("${%s.spec.sourceFile}", uploadID)}, nil
}

// sourceChart derives the chart name and version from the source access, so a chart is
// deployed under the name and version it already has: a Helm access carries them in
// helmChart (name:version, a separate version, or a packaged file name name-version.tgz), an
// OCIImage access in the last repository segment and tag of its image reference. It returns
// empty strings when the access does not carry them (e.g. a local blob).
func sourceChart(access runtime.Typed) (name, version string) {
	switch acc := access.(type) {
	case *helmaccessv1.Helm:
		name, version = acc.GetChartName(), acc.GetVersion()
		if file, ok := strings.CutSuffix(name, ".tgz"); ok {
			// The file name is what gets downloaded, so it wins over a separate version.
			if fileName, fileVersion := splitChartFileName(file); fileVersion != "" {
				return fileName, fileVersion
			}
			return file, version
		}
		return name, version
	case *ociv1.OCIImage:
		ref, _, _ := strings.Cut(acc.ImageReference, "@")
		name, version, _ = strings.Cut(ref[strings.LastIndex(ref, "/")+1:], ":")
		return name, version
	default:
		return "", ""
	}
}

// splitChartFileName splits a packaged chart base name such as my-chart-1.2.3-rc.1 at the
// first '-' followed by a digit, the naming helm package uses (<name>-<version>.tgz).
func splitChartFileName(base string) (name, version string) {
	for i := 0; i+1 < len(base); i++ {
		if base[i] == '-' && base[i+1] >= '0' && base[i+1] <= '9' {
			return base[:i], base[i+1:]
		}
	}
	return base, ""
}

// literalOr returns value as a quoted CEL string literal, or fallback when value is empty.
func literalOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return strconv.Quote(value)
}
