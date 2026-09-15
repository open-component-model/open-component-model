package maven

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const snapMeta = `<metadata>
  <versioning>
    <snapshot><timestamp>20240101.120000</timestamp><buildNumber>3</buildNumber></snapshot>
    <snapshotVersions>
      <snapshotVersion><extension>jar</extension><value>1.0-20240101.120000-3</value></snapshotVersion>
      <snapshotVersion><classifier>sources</classifier><extension>jar</extension><value>1.0-20240101.120000-3</value></snapshotVersion>
      <snapshotVersion><extension>pom</extension><value>1.0-20240101.120000-3</value></snapshotVersion>
    </snapshotVersions>
  </versioning>
</metadata>`

func TestParseMetadata_Snapshot(t *testing.T) {
	md, err := parseMetadata([]byte(snapMeta))
	require.NoError(t, err)
	assert.Equal(t, "3", md.Versioning.Snapshot.BuildNumber)
	assert.Len(t, md.Versioning.SnapshotVersions, 3)
}

func TestParseMetadata_Malformed(t *testing.T) {
	for name, body := range map[string]string{
		"unclosed element": "<metadata><versioning>",
		"html error page":  "<html><body>502 Bad Gateway</body></html><",
		"empty body":       "",
	} {
		_, err := parseMetadata([]byte(body))
		require.ErrorContains(t, err, "error parsing maven-metadata.xml", name)
	}
}

func TestSnapshotFileVersion_FromSnapshotVersions(t *testing.T) {
	md, err := parseMetadata([]byte(snapMeta))
	require.NoError(t, err)
	got, err := snapshotFileVersion(md, "1.0-SNAPSHOT", "sources", "jar")
	require.NoError(t, err)
	assert.Equal(t, "1.0-20240101.120000-3", got)
}

func TestSnapshotFileVersion_FromTimestampFallback(t *testing.T) {
	md := &metadata{}
	md.Versioning.Snapshot.Timestamp = "20240101.120000"
	md.Versioning.Snapshot.BuildNumber = "3"
	got, err := snapshotFileVersion(md, "1.0-SNAPSHOT", "nomatch", "zip")
	require.NoError(t, err)
	assert.Equal(t, "1.0-20240101.120000-3", got)
}

func TestSnapshotFileVersion_Tier3Fallback(t *testing.T) {
	// Empty metadata with no snapshotVersions and no timestamp/buildNumber
	got, err := snapshotFileVersion(&metadata{}, "1.0-SNAPSHOT", "", "jar")
	require.NoError(t, err)
	assert.Equal(t, "1.0-SNAPSHOT", got)
}

func TestSnapshotFileVersion_PoisonedExactMatch_Errors(t *testing.T) {
	md, err := parseMetadata([]byte(`<metadata>
  <versioning>
    <snapshotVersions>
      <snapshotVersion><classifier>sources</classifier><extension>jar</extension><value>../../../evil</value></snapshotVersion>
    </snapshotVersions>
  </versioning>
</metadata>`))
	require.NoError(t, err)
	_, err = snapshotFileVersion(md, "1.0-SNAPSHOT", "sources", "jar")
	require.ErrorContains(t, err, "unsafe value")
}

func TestSnapshotFileVersion_PoisonedTimestampFallback_Errors(t *testing.T) {
	md := &metadata{}
	md.Versioning.Snapshot.Timestamp = "../evil"
	md.Versioning.Snapshot.BuildNumber = "3"
	_, err := snapshotFileVersion(md, "1.0-SNAPSHOT", "nomatch", "zip")
	require.ErrorContains(t, err, "unsafe value")
}

func TestSnapshotFileVersion_PoisonedBuildNumberFallback_Errors(t *testing.T) {
	md := &metadata{}
	md.Versioning.Snapshot.Timestamp = "20240101.120000"
	md.Versioning.Snapshot.BuildNumber = "../evil"
	_, err := snapshotFileVersion(md, "1.0-SNAPSHOT", "nomatch", "zip")
	require.ErrorContains(t, err, "unsafe value")
}

func TestValidateSnapshotVersionValues(t *testing.T) {
	cases := []struct {
		name    string
		sv      snapshotVersion
		wantErr bool
	}{
		{"clean", snapshotVersion{Value: "1.0-20240101.120000-3", Classifier: "sources", Extension: "jar"}, false},
		{"value path traversal", snapshotVersion{Value: "../../../evil", Extension: "jar"}, true},
		{"value slash", snapshotVersion{Value: "foo/bar", Extension: "jar"}, true},
		{"value backslash", snapshotVersion{Value: "foo\\bar", Extension: "jar"}, true},
		{"classifier traversal", snapshotVersion{Value: "1.0", Classifier: "../evil", Extension: "jar"}, true},
		{"extension traversal", snapshotVersion{Value: "1.0", Extension: "../evil"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSnapshotVersionValues(tc.sv)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
