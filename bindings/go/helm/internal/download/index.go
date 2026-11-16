package download

import (
	"fmt"
	"strings"

	helmrepo "helm.sh/helm/v4/pkg/repo/v1"
)

// maxReportedVersions bounds the version list in the error of [ChartVersionFromIndex];
// repository indices routinely carry hundreds of versions per chart.
const maxReportedVersions = 10

// ChartVersionFromIndex looks a chart version up in a Helm repository index.
//
// A requested version must match the index entry exactly. Helm's own
// [helmrepo.IndexFile.Get] interprets the version as a SemVer *constraint* and falls
// back to the closest version it matches ("1.x" or "^1.0.0" select the newest 1.y.z),
// which would silently bind a resource to a chart that was never requested.
//
// An empty version selects the newest version in the index, which is helm's behaviour
// for an unversioned chart reference. Callers that persist the result (digest processing)
// must pin the resolved version so that a later download resolves the same chart.
func ChartVersionFromIndex(index *helmrepo.IndexFile, chartName, version string) (*helmrepo.ChartVersion, error) {
	versions, ok := index.Entries[chartName]
	if !ok || len(versions) == 0 {
		return nil, fmt.Errorf("chart %q not found in repository index", chartName)
	}

	if version == "" {
		cv, err := index.Get(chartName, "")
		if err != nil {
			return nil, fmt.Errorf("no version of chart %q found in repository index: %w", chartName, err)
		}
		return cv, nil
	}

	for _, cv := range versions {
		if cv == nil || cv.Metadata == nil {
			continue
		}
		if cv.Version == version {
			return cv, nil
		}
	}

	return nil, fmt.Errorf("chart %q version %q not found in repository index (available: %s)",
		chartName, version, availableVersions(versions))
}

func availableVersions(versions helmrepo.ChartVersions) string {
	listed := make([]string, 0, min(len(versions), maxReportedVersions))
	for _, cv := range versions {
		if cv == nil || cv.Metadata == nil || cv.Version == "" {
			continue
		}
		if len(listed) == maxReportedVersions {
			return strings.Join(listed, ", ") + ", ..."
		}
		listed = append(listed, cv.Version)
	}
	if len(listed) == 0 {
		return "none"
	}
	return strings.Join(listed, ", ")
}
