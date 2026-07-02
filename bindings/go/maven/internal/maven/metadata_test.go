package maven

import (
	"testing"

	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
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
	if err != nil {
		t.Fatal(err)
	}
	if md.Versioning.Snapshot.BuildNumber != "3" {
		t.Fatalf("buildNumber: %q", md.Versioning.Snapshot.BuildNumber)
	}
	if len(md.Versioning.SnapshotVersions) != 3 {
		t.Fatalf("snapshotVersions: %d", len(md.Versioning.SnapshotVersions))
	}
}

func TestSnapshotFileVersion_FromSnapshotVersions(t *testing.T) {
	md, _ := parseMetadata([]byte(snapMeta))
	got, err := snapshotFileVersion(md, "1.0-SNAPSHOT", "sources", "jar")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.0-20240101.120000-3" {
		t.Fatalf("got %q", got)
	}
}

func TestSnapshotFileVersion_FromTimestampFallback(t *testing.T) {
	md := &metadata{}
	md.Versioning.Snapshot.Timestamp = "20240101.120000"
	md.Versioning.Snapshot.BuildNumber = "3"
	got, err := snapshotFileVersion(md, "1.0-SNAPSHOT", "nomatch", "zip")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.0-20240101.120000-3" {
		t.Fatalf("got %q", got)
	}
}

func TestSnapshotFileVersion_Tier3Fallback(t *testing.T) {
	// Empty metadata with no snapshotVersions and no timestamp/buildNumber
	md := &metadata{}
	got, err := snapshotFileVersion(md, "1.0-SNAPSHOT", "", "jar")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.0-SNAPSHOT" {
		t.Fatalf("tier 3 fallback want %q, got %q", "1.0-SNAPSHOT", got)
	}
}

func TestSnapshotFileVersion_PoisonedExactMatch_Errors(t *testing.T) {
	md, _ := parseMetadata([]byte(`<metadata>
  <versioning>
    <snapshotVersions>
      <snapshotVersion><classifier>sources</classifier><extension>jar</extension><value>../../../evil</value></snapshotVersion>
    </snapshotVersions>
  </versioning>
</metadata>`))
	_, err := snapshotFileVersion(md, "1.0-SNAPSHOT", "sources", "jar")
	if err == nil {
		t.Fatal("expected error for poisoned exact-match snapshotVersion value")
	}
}

func TestSnapshotFileVersion_PoisonedTimestampFallback_Errors(t *testing.T) {
	md := &metadata{}
	md.Versioning.Snapshot.Timestamp = "../evil"
	md.Versioning.Snapshot.BuildNumber = "3"
	_, err := snapshotFileVersion(md, "1.0-SNAPSHOT", "nomatch", "zip")
	if err == nil {
		t.Fatal("expected error for poisoned snapshot timestamp")
	}
}

func TestSnapshotFileVersion_PoisonedBuildNumberFallback_Errors(t *testing.T) {
	md := &metadata{}
	md.Versioning.Snapshot.Timestamp = "20240101.120000"
	md.Versioning.Snapshot.BuildNumber = "../evil"
	_, err := snapshotFileVersion(md, "1.0-SNAPSHOT", "nomatch", "zip")
	if err == nil {
		t.Fatal("expected error for poisoned snapshot buildNumber")
	}
}

func TestFilterSnapshotVersions(t *testing.T) {
	md, _ := parseMetadata([]byte(snapMeta))
	// bare GAV (classifier nil, extension nil) -> all resource entries
	all := filterSnapshotVersions(&v1.Maven{}, md.Versioning.SnapshotVersions)
	if len(all) != 3 {
		t.Fatalf("bare GAV want 3, got %d", len(all))
	}
	// extension jar only -> main jar + sources jar (2)
	jars := filterSnapshotVersions(&v1.Maven{Extension: ptr("jar")}, md.Versioning.SnapshotVersions)
	if len(jars) != 2 {
		t.Fatalf("ext jar want 2, got %d", len(jars))
	}
	// classifier "" (main only) + ext jar -> 1
	main := filterSnapshotVersions(&v1.Maven{Classifier: ptr(""), Extension: ptr("jar")}, md.Versioning.SnapshotVersions)
	if len(main) != 1 {
		t.Fatalf("main jar want 1, got %d", len(main))
	}
	// classifier "sources" + extension nil -> 1 (sources jar only)
	sources := filterSnapshotVersions(&v1.Maven{Classifier: ptr("sources")}, md.Versioning.SnapshotVersions)
	if len(sources) != 1 {
		t.Fatalf("sources classifier, nil ext want 1, got %d", len(sources))
	}
	// classifier "sources" + extension "jar" -> 1
	sourcesJar := filterSnapshotVersions(&v1.Maven{Classifier: ptr("sources"), Extension: ptr("jar")}, md.Versioning.SnapshotVersions)
	if len(sourcesJar) != 1 {
		t.Fatalf("sources classifier, jar ext want 1, got %d", len(sourcesJar))
	}
	// classifier "nomatch" + extension nil -> 0 (no matches)
	noMatch := filterSnapshotVersions(&v1.Maven{Classifier: ptr("nomatch")}, md.Versioning.SnapshotVersions)
	if len(noMatch) != 0 {
		t.Fatalf("nomatch classifier want 0, got %d", len(noMatch))
	}
	// extension "" (explicitly empty) defaults to "jar", same as ext "jar" -> 2
	emptyExt := filterSnapshotVersions(&v1.Maven{Extension: ptr("")}, md.Versioning.SnapshotVersions)
	if len(emptyExt) != 2 {
		t.Fatalf("empty extension (defaults to jar) want 2, got %d", len(emptyExt))
	}
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
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSnapshotFileVersion_ExtensionDefaulting(t *testing.T) {
	md, _ := parseMetadata([]byte(snapMeta))
	// Empty extension "" should default to "jar" and match the snapshotVersion entry
	got, err := snapshotFileVersion(md, "1.0-SNAPSHOT", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.0-20240101.120000-3" {
		t.Fatalf("extension defaulting want %q, got %q", "1.0-20240101.120000-3", got)
	}
}
