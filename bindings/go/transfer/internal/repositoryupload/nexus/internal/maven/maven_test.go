package maven_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/nexus/internal/maven"
)

func TestParsePath(t *testing.T) {
	tests := []struct {
		path    string
		want    maven.Coordinates
		wantErr bool
	}{
		{path: "com/example/demo/1.0.0/demo-1.0.0.jar", want: maven.Coordinates{GroupID: "com.example", ArtifactID: "demo", Version: "1.0.0", Extension: "jar"}},
		{path: "com/example/demo/1.0.0/demo-1.0.0.pom", want: maven.Coordinates{GroupID: "com.example", ArtifactID: "demo", Version: "1.0.0", Extension: "pom"}},
		{path: "com/example/demo/1.0.0/demo-1.0.0-sources.jar", want: maven.Coordinates{GroupID: "com.example", ArtifactID: "demo", Version: "1.0.0", Classifier: "sources", Extension: "jar"}},
		{path: "org/demo/2.0/demo-2.0.tar.gz", want: maven.Coordinates{GroupID: "org", ArtifactID: "demo", Version: "2.0", Extension: "tar.gz"}},
		{path: "demo/1.0.0/demo-1.0.0.jar", wantErr: true},
		{path: "com/example/demo/1.0.0/demo-1.0.0", wantErr: true},
		{path: "com/example/demo/1.0.0/demo-1.0.0-.jar", wantErr: true},
		{path: "com/example/demo/1.0.0/demo-1.0.0-sources", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			r := require.New(t)
			got, err := maven.ParsePath(tc.path)
			if tc.wantErr {
				r.ErrorContains(err, "Maven repository layout")
				return
			}
			r.NoError(err)
			r.Equal(tc.want, got)
		})
	}
}
