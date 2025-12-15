package test

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fluxcd/pkg/runtime/conditions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

func SetupRepositoryWithSpecData(
	ctx context.Context,
	k8sClient client.Client,
	namespace, repositoryName string,
	specData []byte,
) *v1alpha1.Repository {
	GinkgoHelper()

	repository := &v1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      repositoryName,
		},
		Spec: v1alpha1.RepositorySpec{
			RepositorySpec: &apiextensionsv1.JSON{
				Raw: specData,
			},
			Interval: metav1.Duration{Duration: time.Second * 5},
		},
	}
	Expect(k8sClient.Create(ctx, repository)).To(Succeed())

	conditions.MarkTrue(repository, "Ready", "ready", "message")
	Expect(k8sClient.Status().Update(ctx, repository)).To(Succeed())

	return repository
}

func SetupCTFComponentVersionRepository(ctx SpecContext, ctfpath string, descs []*descriptor.Descriptor) (*oci.Repository, []byte) {
	GinkgoHelper()

	repoSpec := &ctf.Repository{Type: runtime.Type{Version: "v1", Name: "ctf"}, FilePath: ctfpath, AccessMode: ctf.AccessModeReadWrite}
	repo, err := ocirepository.NewFromCTFRepoV1(ctx, repoSpec)
	Expect(err).NotTo(HaveOccurred())

	for _, desc := range descs {
		Expect(repo.AddComponentVersion(ctx, desc)).To(Succeed())
	}

	specData, err := json.Marshal(repoSpec)
	Expect(err).NotTo(HaveOccurred())

	return repo, specData
}
