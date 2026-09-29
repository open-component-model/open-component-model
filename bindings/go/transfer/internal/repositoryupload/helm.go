package repositoryupload

import (
	"fmt"
	"strings"

	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// HelmAccess returns the Helm/v1 access of chart name:version in helmRepo; it rejects a name containing
// ":" or "/" and a version containing "/", which server recorded for the chart at url.
func HelmAccess(server, helmRepo, name, version, url string) (runtime.Typed, error) {
	if strings.ContainsAny(name, ":/") || strings.Contains(version, "/") {
		return nil, fmt.Errorf("%s recorded an invalid chart name %q or version %q for %s", server, name, version, url)
	}
	return &helmaccessv1.Helm{
		Type:           runtime.NewVersionedType(helmaccessv1.Type, helmaccessv1.Version),
		HelmRepository: helmRepo,
		HelmChart:      name + ":" + version,
	}, nil
}
