package maven

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
)

func TestResolve_ReleaseBareGAV_MainJar(t *testing.T) {
	m := &v1.Maven{RepoURL: "https://r/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"}
	refs, err := NewClient(nil).Resolve(context.Background(), m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Filename != "lib-1.2.3.jar" {
		t.Fatalf("refs: %+v", refs)
	}
}

func TestResolve_ReleasePartial_Errors(t *testing.T) {
	m := &v1.Maven{RepoURL: "https://r/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3", Extension: ptr("jar")}
	_, err := NewClient(nil).Resolve(context.Background(), m, nil)
	if err == nil {
		t.Fatal("expected error for release + partial selector")
	}
}

func TestResolve_SnapshotPattern_MultiFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml" {
			_, _ = w.Write([]byte(snapMeta))
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	m := &v1.Maven{RepoURL: srv.URL + "/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.0-SNAPSHOT", Extension: ptr("jar")}
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 { // main jar + sources jar
		t.Fatalf("want 2 refs, got %d: %+v", len(refs), refs)
	}
	if refs[0].Filename != "lib-1.0-20240101.120000-3.jar" {
		t.Fatalf("timestamped filename expected, got %s", refs[0].Filename)
	}
}

func TestResolve_SnapshotPattern_SingleMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml" {
			_, _ = w.Write([]byte(snapMeta))
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	// classifier "sources" + extension nil matches exactly one snapshotVersion
	// entry in snapMeta: this is still a "pattern" resolution (neither
	// classifier nor extension is fully pinned), but happens to yield a single
	// file.
	m := &v1.Maven{RepoURL: srv.URL + "/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.0-SNAPSHOT", Classifier: ptr("sources")}
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("want 1 ref, got %d: %+v", len(refs), refs)
	}
	if refs[0].Filename != "lib-1.0-20240101.120000-3-sources.jar" {
		t.Fatalf("timestamped filename expected, got %s", refs[0].Filename)
	}
}

func TestResolve_SnapshotPattern_SingleMatch_MediaTypeOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml" {
			_, _ = w.Write([]byte(snapMeta))
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	m := &v1.Maven{
		RepoURL:    srv.URL + "/maven2",
		GroupID:    "com.example",
		ArtifactID: "lib",
		Version:    "1.0-SNAPSHOT",
		Classifier: ptr("sources"),
		MediaType:  ptr("application/x-custom"),
	}
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("want 1 ref, got %d: %+v", len(refs), refs)
	}
	if refs[0].MediaType != "application/x-custom" {
		t.Fatalf("want MediaType override applied, got %q", refs[0].MediaType)
	}
}

func TestResolve_SnapshotPattern_PoisonedMetadata_Errors(t *testing.T) {
	const poisoned = `<metadata>
  <versioning>
    <snapshot><timestamp>20240101.120000</timestamp><buildNumber>3</buildNumber></snapshot>
    <snapshotVersions>
      <snapshotVersion><extension>jar</extension><value>../../../evil</value></snapshotVersion>
    </snapshotVersions>
  </versioning>
</metadata>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml" {
			_, _ = w.Write([]byte(poisoned))
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	m := &v1.Maven{RepoURL: srv.URL + "/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.0-SNAPSHOT", Extension: ptr("jar")}
	_, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	if err == nil {
		t.Fatal("expected error for poisoned snapshotVersion value")
	}
}

func TestResolve_SnapshotFile_PoisonedExactMatch_Errors(t *testing.T) {
	const poisoned = `<metadata>
  <versioning>
    <snapshot><timestamp>20240101.120000</timestamp><buildNumber>3</buildNumber></snapshot>
    <snapshotVersions>
      <snapshotVersion><extension>jar</extension><value>../../../evil</value></snapshotVersion>
    </snapshotVersions>
  </versioning>
</metadata>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml" {
			_, _ = w.Write([]byte(poisoned))
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	// Both classifier and extension set -> IsFile path, exact-match tier.
	m := &v1.Maven{RepoURL: srv.URL + "/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.0-SNAPSHOT", Classifier: ptr(""), Extension: ptr("jar")}
	_, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	if err == nil {
		t.Fatal("expected error for poisoned snapshotVersion value on the IsFile path")
	}
}

func TestResolve_SnapshotFile_PoisonedTimestamp_Errors(t *testing.T) {
	const poisoned = `<metadata>
  <versioning>
    <snapshot><timestamp>../evil</timestamp><buildNumber>3</buildNumber></snapshot>
  </versioning>
</metadata>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml" {
			_, _ = w.Write([]byte(poisoned))
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	// Both classifier and extension set -> IsFile path; no matching
	// snapshotVersion entry, falls through to the timestamp+buildNumber tier.
	m := &v1.Maven{RepoURL: srv.URL + "/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.0-SNAPSHOT", Classifier: ptr(""), Extension: ptr("jar")}
	_, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	if err == nil {
		t.Fatal("expected error for poisoned snapshot timestamp on the IsFile path")
	}
}

func TestResolve_LATEST_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/maven2/com/example/lib/maven-metadata.xml" {
			_, _ = w.Write([]byte(`<metadata><versioning><latest>1.2.3</latest><release>1.2.2</release></versioning></metadata>`))
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	m := &v1.Maven{RepoURL: srv.URL + "/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "LATEST"}
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Filename != "lib-1.2.3.jar" {
		t.Fatalf("refs: %+v", refs)
	}
}

func TestResolve_RELEASE_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/maven2/com/example/lib/maven-metadata.xml" {
			_, _ = w.Write([]byte(`<metadata><versioning><latest>1.2.3</latest><release>1.2.2</release></versioning></metadata>`))
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	m := &v1.Maven{RepoURL: srv.URL + "/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "RELEASE"}
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Filename != "lib-1.2.2.jar" {
		t.Fatalf("refs: %+v", refs)
	}
}

func TestResolve_LATEST_ErrorPath_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.NotFound(w, req)
	}))
	defer srv.Close()
	m := &v1.Maven{RepoURL: srv.URL + "/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "LATEST"}
	_, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	if err == nil {
		t.Fatal("expected error for LATEST with missing metadata")
	}
}

func TestResolve_LATEST_ErrorPath_EmptyVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/maven2/com/example/lib/maven-metadata.xml" {
			_, _ = w.Write([]byte(`<metadata><versioning></versioning></metadata>`))
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	m := &v1.Maven{RepoURL: srv.URL + "/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "LATEST"}
	_, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	if err == nil {
		t.Fatal("expected error for LATEST with empty version")
	}
}

func TestResolve_Snapshot_404Fallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.NotFound(w, req)
	}))
	defer srv.Close()
	m := &v1.Maven{
		RepoURL:    srv.URL + "/maven2",
		GroupID:    "com.example",
		ArtifactID: "lib",
		Version:    "1.0-SNAPSHOT",
		Classifier: ptr(""),
		Extension:  ptr("jar"),
	}
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Filename != "lib-1.0-SNAPSHOT.jar" {
		t.Fatalf("expected literal fallback, got: %+v", refs)
	}
}

func TestResolve_Snapshot_PatternZeroMatches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml" {
			_, _ = w.Write([]byte(snapMeta))
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	m := &v1.Maven{
		RepoURL:    srv.URL + "/maven2",
		GroupID:    "com.example",
		ArtifactID: "lib",
		Version:    "1.0-SNAPSHOT",
		Classifier: ptr("nomatch"),
	}
	_, err := NewClient(srv.Client()).Resolve(context.Background(), m, nil)
	if err == nil {
		t.Fatal("expected error for zero-matches snapshot pattern")
	}
}

func TestResolve_Release_SymmetricXOR(t *testing.T) {
	m := &v1.Maven{
		RepoURL:    "https://r/maven2",
		GroupID:    "com.example",
		ArtifactID: "lib",
		Version:    "1.2.3",
		Classifier: ptr("sources"),
	}
	_, err := NewClient(nil).Resolve(context.Background(), m, nil)
	if err == nil {
		t.Fatal("expected error for release with classifier but no extension")
	}
}
