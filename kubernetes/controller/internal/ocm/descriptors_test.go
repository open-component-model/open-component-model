package ocm_test

import (
	. "github.com/mandelsoft/goutils/testutils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/ocm/api/ocm"
	"ocm.software/ocm/api/ocm/compdesc"
	"ocm.software/ocm/api/ocm/extensions/repositories/composition"
	"ocm.software/ocm/api/utils/blobaccess"
	"ocm.software/ocm/api/utils/mime"
	"ocm.software/ocm/api/utils/runtime"

	v1 "ocm.software/ocm/api/ocm/compdesc/meta/v1"
	resourcetypes "ocm.software/ocm/api/ocm/extensions/artifacttypes"

	k8socm "ocm.software/open-component-model/kubernetes/controller/internal/ocm"
)

var _ = Describe("types test", func() {
	It("marshal and unmarshal components", func() {
		cv := composition.NewComponentVersion(ocm.DefaultContext(), "acme.org/test", "1.0.0")
		MustBeSuccessful(cv.SetResourceBlob(ocm.NewResourceMeta("test-resource", resourcetypes.PLAIN_TEXT, v1.LocalRelation), blobaccess.ForString(mime.MIME_TEXT, "this is a test"), "", nil))
		descriptor := cv.GetDescriptor()
		list := k8socm.Descriptors{List: []*compdesc.ComponentDescriptor{descriptor}}
		data := Must(runtime.DefaultYAMLEncoding.Marshal(list))
		Expect(data).To(YAMLEqual(`
components:
- component:
    componentReferences: []
    name: acme.org/test
    provider: acme
    repositoryContexts: []
    resources:
    - access:
        localReference: 2e99758548972a8e8822ad47fa1017ff72f06f3ff6a016851f45c398732bc50c
        mediaType: text/plain
        type: localBlob
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: 2e99758548972a8e8822ad47fa1017ff72f06f3ff6a016851f45c398732bc50c
      name: test-resource
      relation: local
      type: plainText
      version: 1.0.0
    sources: []
    version: 1.0.0
  meta:
    schemaVersion: v2
`))
		var decoded k8socm.Descriptors
		MustBeSuccessful(runtime.DefaultYAMLEncoding.Unmarshal(data, &decoded))
		Expect(decoded).To(YAMLEqual(list))
	})
})
