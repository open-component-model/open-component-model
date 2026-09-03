package externalartifact

import (
	"context"
	"testing"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"
)

// discoveryWith builds a fake discovery client advertising the given resource lists.
func discoveryWith(lists ...*metav1.APIResourceList) discovery.DiscoveryInterface {
	return &fakediscovery.FakeDiscovery{Fake: &clienttesting.Fake{Resources: lists}}
}

func TestCheckCRDInstalledPresent(t *testing.T) {
	dc := discoveryWith(&metav1.APIResourceList{
		GroupVersion: sourcev1.GroupVersion.String(),
		APIResources: []metav1.APIResource{{Kind: sourcev1.ExternalArtifactKind, Name: "externalartifacts"}},
	})
	if err := checkCRDInstalledWith(context.Background(), dc); err != nil {
		t.Errorf("expected CRD present, got error: %v", err)
	}
}

func TestCheckCRDInstalledMissingGroup(t *testing.T) {
	// No source.toolkit.fluxcd.io group at all.
	dc := discoveryWith()
	if err := checkCRDInstalledWith(context.Background(), dc); err == nil {
		t.Error("expected error when the Flux group is absent")
	}
}

func TestCheckCRDInstalledGroupWithoutKind(t *testing.T) {
	// The group exists (other Flux sources) but not ExternalArtifact.
	dc := discoveryWith(&metav1.APIResourceList{
		GroupVersion: sourcev1.GroupVersion.String(),
		APIResources: []metav1.APIResource{{Kind: "GitRepository", Name: "gitrepositories"}},
	})
	if err := checkCRDInstalledWith(context.Background(), dc); err == nil {
		t.Error("expected error when ExternalArtifact kind is absent")
	}
}
