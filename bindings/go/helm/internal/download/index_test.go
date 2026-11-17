package download

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	helmrepo "helm.sh/helm/v4/pkg/repo/v1"
)

func chartVersion(version string) *helmrepo.ChartVersion {
	return &helmrepo.ChartVersion{Metadata: &chartv2.Metadata{APIVersion: "v2", Name: "mychart", Version: version}}
}

func indexWith(versions ...string) *helmrepo.IndexFile {
	index := helmrepo.NewIndexFile()
	entries := make(helmrepo.ChartVersions, 0, len(versions))
	for _, version := range versions {
		entries = append(entries, chartVersion(version))
	}
	index.Entries["mychart"] = entries
	return index
}

func TestChartVersionFromIndex(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		index       *helmrepo.IndexFile
		chart       string
		version     string
		wantVersion string
		wantErr     string
	}{
		{
			name:        "exact version",
			index:       indexWith("1.206.0", "1.0.0"),
			chart:       "mychart",
			version:     "1.0.0",
			wantVersion: "1.0.0",
		},
		{
			name:        "build metadata is part of the version",
			index:       indexWith("0.1.0+abc123"),
			chart:       "mychart",
			version:     "0.1.0+abc123",
			wantVersion: "0.1.0+abc123",
		},
		{
			name:    "a version differing only in build metadata is not a match",
			index:   indexWith("0.1.0+abc123"),
			chart:   "mychart",
			version: "0.1.0",
			wantErr: `chart "mychart" version "0.1.0" not found in repository index (available: 0.1.0+abc123)`,
		},
		{
			name:        "a leading v is not part of the version",
			index:       indexWith("1.0.0"),
			chart:       "mychart",
			version:     "v1.0.0",
			wantVersion: "1.0.0",
		},
		{
			name:        "a v-prefixed index entry matches an unprefixed request",
			index:       indexWith("v1.0.0"),
			chart:       "mychart",
			version:     "1.0.0",
			wantVersion: "v1.0.0",
		},
		{
			name:        "an exact match wins over a v-prefixed one",
			index:       indexWith("v1.0.0", "1.0.0"),
			chart:       "mychart",
			version:     "1.0.0",
			wantVersion: "1.0.0",
		},
		{
			name:    "a v prefix does not make a constraint resolvable",
			index:   indexWith("1.206.0"),
			chart:   "mychart",
			version: "v1.x",
			wantErr: `chart "mychart" version "v1.x" not found in repository index (available: 1.206.0)`,
		},
		{
			// Regression: helm's IndexFile.Get reads the version as a SemVer constraint and
			// returns the closest match, binding a chart that was never requested.
			name:    "semver range",
			index:   indexWith("1.206.0"),
			chart:   "mychart",
			version: "1.x",
			wantErr: `chart "mychart" version "1.x" not found in repository index (available: 1.206.0)`,
		},
		{
			name:    "caret constraint",
			index:   indexWith("1.206.0"),
			chart:   "mychart",
			version: "^1.0.0",
			wantErr: `version "^1.0.0" not found in repository index`,
		},
		{
			name:    "non-semver version that is not in the index",
			index:   indexWith("1.206.0"),
			chart:   "mychart",
			version: "1.0-287.87c9d32.master.309",
			wantErr: `version "1.0-287.87c9d32.master.309" not found in repository index`,
		},
		{
			name:        "empty version selects the newest version",
			index:       indexWith("0.1.0", "1.206.0", "1.0.0"),
			chart:       "mychart",
			version:     "",
			wantVersion: "1.206.0",
		},
		{
			name:    "unknown chart",
			index:   indexWith("1.0.0"),
			chart:   "other",
			version: "1.0.0",
			wantErr: `chart "other" not found in repository index`,
		},
		{
			name:    "chart without entries",
			index:   indexWith(),
			chart:   "mychart",
			version: "1.0.0",
			wantErr: `chart "mychart" not found in repository index`,
		},
		{
			name:    "empty version with only unparsable versions",
			index:   indexWith("not-a-version"),
			chart:   "mychart",
			version: "",
			wantErr: `no version of chart "mychart" found in repository index`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// LoadIndexFile sorts and defaults entries; do the same so the "newest" selection
			// matches what a repository index yields in practice.
			tc.index.SortEntries()

			cv, err := ChartVersionFromIndex(tc.index, tc.chart, tc.version)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Nil(t, cv)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, cv)
			assert.Equal(t, tc.wantVersion, cv.Version)
		})
	}

	t.Run("entries without metadata are skipped", func(t *testing.T) {
		t.Parallel()
		index := helmrepo.NewIndexFile()
		index.Entries["mychart"] = helmrepo.ChartVersions{nil, {}, chartVersion("1.0.0")}

		cv, err := ChartVersionFromIndex(index, "mychart", "1.0.0")
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", cv.Version)

		_, err = ChartVersionFromIndex(index, "mychart", "2.0.0")
		require.ErrorContains(t, err, "(available: 1.0.0)")
	})

	t.Run("an index entry carrying no usable version reports none", func(t *testing.T) {
		t.Parallel()
		index := helmrepo.NewIndexFile()
		index.Entries["mychart"] = helmrepo.ChartVersions{{Metadata: &chartv2.Metadata{Name: "mychart"}}}

		_, err := ChartVersionFromIndex(index, "mychart", "1.0.0")
		require.ErrorContains(t, err, "(available: none)")
	})

	t.Run("the reported version list is bounded and counts the rest", func(t *testing.T) {
		t.Parallel()
		versions := make([]string, 0, maxReportedVersions+5)
		for i := range cap(versions) {
			versions = append(versions, fmt.Sprintf("1.%d.0", i))
		}

		_, err := ChartVersionFromIndex(indexWith(versions...), "mychart", "9.9.9")
		require.Error(t, err)
		available := err.Error()[strings.Index(err.Error(), "(available: ")+len("(available: "):]
		assert.Equal(t, strings.Join(versions[:maxReportedVersions], ", ")+" and 5 more)", available)
	})
}
