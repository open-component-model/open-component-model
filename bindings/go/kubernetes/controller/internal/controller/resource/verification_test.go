package resource

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/opencontainers/go-digest"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ = Describe("Resource Controller verification", func() {
	// A local blob is checked against the digest the repository stores it under, so a
	// descriptor recording another digest fails without the blob being downloaded.
	DescribeTable("verifies a local blob against its descriptor digest",
		func(ctx SpecContext, recordOtherDigest bool) {
			name := test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentName := "ocm.software/local-verification-" + name
			namespace := test.NamespaceForTest(ctx)
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			By("storing a component version with a local blob in a CTF")
			ctfPath := GinkgoT().TempDir()
			fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
			Expect(err).NotTo(HaveOccurred())
			repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))), oci.WithTempDir(GinkgoT().TempDir()))
			Expect(err).NotTo(HaveOccurred())

			res := &descruntime.Resource{
				ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "data", Version: "v1.0.0"}},
				Type:        "plainText",
				Relation:    descruntime.LocalRelation,
				Access: &v2.LocalBlob{
					Type:      runtime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
					MediaType: "text/plain",
				},
			}
			stored, err := repo.AddLocalResource(ctx, componentName, "v1.0.0", res, inmemory.New(bytes.NewReader([]byte("content"))))
			Expect(err).NotTo(HaveOccurred())
			if recordOtherDigest {
				stored.Digest.Value = digest.FromString("other").Encoded()
			}
			Expect(repo.AddComponentVersion(ctx, &descruntime.Descriptor{
				Meta: descruntime.Meta{Version: "v2"},
				Component: descruntime.Component{
					ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: "v1.0.0"}},
					Provider:      descruntime.Provider{Name: "ocm.software"},
					Resources:     []descruntime.Resource{*stored},
				},
			})).To(Succeed())
			specData, err := json.Marshal(&ctfv1.Repository{
				Type:       runtime.Type{Name: "ctf", Version: "v1"},
				FilePath:   ctfPath,
				AccessMode: ctfv1.AccessModeReadOnly,
			})
			Expect(err).NotTo(HaveOccurred())

			componentObj := test.MockComponent(ctx, "component-"+name, namespace.GetName(), &test.MockComponentOptions{
				Client:   k8sClient,
				Recorder: recorder,
				Info: v1alpha1.ComponentInfo{
					Component:      componentName,
					Version:        "v1.0.0",
					RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
				},
				Repository: "ocm.software/test-repository",
			})
			DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, componentObj) })

			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{Name: "resource-" + name, Namespace: namespace.GetName()},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{Name: componentObj.GetName()},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{Resource: runtime.Identity{"name": "data"}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, resourceObj) })

			if recordOtherDigest {
				test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, resourceObj, v1alpha1.GetOCMResourceFailedReason, "digest mismatch")
				return
			}
			test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{"Status.Resource.Name": "data"})
		},
		Entry("the recorded digest", false),
		Entry("another recorded digest", true),
	)

	// Under Always a selected resource without a usable recorded digest fails before any download:
	// the controller never manufactures a missing digest.
	DescribeTable("requires a recorded digest under Always",
		func(ctx SpecContext, mutate func(d *descruntime.Digest) *descruntime.Digest, policy v1alpha1.VerificationPolicy, reason, message, extraMessage string) {
			name := test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentName := "ocm.software/require-digest-" + name
			namespace := test.NamespaceForTest(ctx)
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			ctfPath := GinkgoT().TempDir()
			fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
			Expect(err).NotTo(HaveOccurred())
			repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))), oci.WithTempDir(GinkgoT().TempDir()))
			Expect(err).NotTo(HaveOccurred())

			res := &descruntime.Resource{
				ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "data", Version: "v1.0.0"}},
				Type:        "plainText",
				Relation:    descruntime.LocalRelation,
				Access: &v2.LocalBlob{
					Type:      runtime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
					MediaType: "text/plain",
				},
			}
			stored, err := repo.AddLocalResource(ctx, componentName, "v1.0.0", res, inmemory.New(bytes.NewReader([]byte("content"))))
			Expect(err).NotTo(HaveOccurred())
			intended := mutate(stored.Digest)
			stored.Digest = intended
			Expect(repo.AddComponentVersion(ctx, &descruntime.Descriptor{
				Meta: descruntime.Meta{Version: "v2"},
				Component: descruntime.Component{
					ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: "v1.0.0"}},
					Provider:      descruntime.Provider{Name: "ocm.software"},
					Resources:     []descruntime.Resource{*stored},
				},
			})).To(Succeed())
			readBack, err := repo.GetComponentVersion(ctx, componentName, "v1.0.0")
			Expect(err).NotTo(HaveOccurred())
			Expect(readBack.Component.Resources[0].Digest).To(Equal(intended))
			specData, err := json.Marshal(&ctfv1.Repository{
				Type:       runtime.Type{Name: "ctf", Version: "v1"},
				FilePath:   ctfPath,
				AccessMode: ctfv1.AccessModeReadOnly,
			})
			Expect(err).NotTo(HaveOccurred())

			componentObj := test.MockComponent(ctx, "component-"+name, namespace.GetName(), &test.MockComponentOptions{
				Client:   k8sClient,
				Recorder: recorder,
				Info: v1alpha1.ComponentInfo{
					Component:      componentName,
					Version:        "v1.0.0",
					RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
				},
				Repository: "ocm.software/test-repository",
			})
			DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, componentObj) })

			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{Name: "resource-" + name, Namespace: namespace.GetName()},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef:       corev1.LocalObjectReference{Name: componentObj.GetName()},
					VerificationPolicy: policy,
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{Resource: runtime.Identity{"name": "data"}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, resourceObj) })

			test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, resourceObj, reason, message)
			if extraMessage != "" {
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObj)).To(Succeed())
				readyCond := apimeta.FindStatusCondition(resourceObj.GetConditions(), v1alpha1.ReadyCondition)
				Expect(readyCond).NotTo(BeNil())
				Expect(readyCond.Message).To(ContainSubstring(extraMessage))
			}
		},
		Entry("nil digest with default Always", func(*descruntime.Digest) *descruntime.Digest { return nil },
			v1alpha1.VerificationPolicy(""), v1alpha1.GetOCMResourceFailedReason, "has no digest to verify", ""),
		Entry("empty digest with explicit Always", func(*descruntime.Digest) *descruntime.Digest { return &descruntime.Digest{} },
			v1alpha1.VerificationPolicyAlways, v1alpha1.GetOCMResourceFailedReason, "has no digest to verify", ""),
		Entry("excluded-from-signature digest", func(*descruntime.Digest) *descruntime.Digest { return descruntime.NewExcludeFromSignatureDigest() },
			v1alpha1.VerificationPolicyAlways, v1alpha1.GetOCMResourceFailedReason, "has no digest to verify", ""),
		Entry("partial digest", func(*descruntime.Digest) *descruntime.Digest { return &descruntime.Digest{HashAlgorithm: "SHA-256"} },
			v1alpha1.VerificationPolicyAlways, v1alpha1.GetOCMResourceFailedReason, "has an invalid digest", "incomplete digest"),
	)

	// A recorded digest keeps its exact spelling: a supported but non-canonical hash algorithm and an
	// uppercase value are preserved, never rewritten into the parser's canonical form.
	It("preserves a recorded digest spelling under Always", func(ctx SpecContext) {
		componentName := "ocm.software/preserve-digest"
		namespace := test.NamespaceForTest(ctx)
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		ctfPath := GinkgoT().TempDir()
		fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
		Expect(err).NotTo(HaveOccurred())
		repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))), oci.WithTempDir(GinkgoT().TempDir()))
		Expect(err).NotTo(HaveOccurred())

		res := &descruntime.Resource{
			ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "data", Version: "v1.0.0"}},
			Type:        "plainText",
			Relation:    descruntime.LocalRelation,
			Access: &v2.LocalBlob{
				Type:      runtime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
				MediaType: "text/plain",
			},
		}
		stored, err := repo.AddLocalResource(ctx, componentName, "v1.0.0", res, inmemory.New(bytes.NewReader([]byte("content"))))
		Expect(err).NotTo(HaveOccurred())
		expected := descruntime.Digest{HashAlgorithm: "sha256", Value: strings.ToUpper(digest.FromString("content").Encoded()), NormalisationAlgorithm: ""}
		fixtureDigest := expected
		stored.Digest = &fixtureDigest
		Expect(repo.AddComponentVersion(ctx, &descruntime.Descriptor{
			Meta: descruntime.Meta{Version: "v2"},
			Component: descruntime.Component{
				ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: "v1.0.0"}},
				Provider:      descruntime.Provider{Name: "ocm.software"},
				Resources:     []descruntime.Resource{*stored},
			},
		})).To(Succeed())
		readBackPre, err := repo.GetComponentVersion(ctx, componentName, "v1.0.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(readBackPre.Component.Resources[0].Digest).To(Equal(&expected))
		specData, err := json.Marshal(&ctfv1.Repository{
			Type:       runtime.Type{Name: "ctf", Version: "v1"},
			FilePath:   ctfPath,
			AccessMode: ctfv1.AccessModeReadOnly,
		})
		Expect(err).NotTo(HaveOccurred())

		componentObj := test.MockComponent(ctx, "component-preserve", namespace.GetName(), &test.MockComponentOptions{
			Client:   k8sClient,
			Recorder: recorder,
			Info: v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        "v1.0.0",
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			},
			Repository: "ocm.software/test-repository",
		})
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, componentObj) })

		resourceObj := &v1alpha1.Resource{
			ObjectMeta: metav1.ObjectMeta{Name: "resource-preserve", Namespace: namespace.GetName()},
			Spec: v1alpha1.ResourceSpec{
				ComponentRef: corev1.LocalObjectReference{Name: componentObj.GetName()},
				Resource: v1alpha1.ResourceID{
					ByReference: v1alpha1.ResourceReference{Resource: runtime.Identity{"name": "data"}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, resourceObj) })

		test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{"Status.Resource.Name": "data"})
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObj)).To(Succeed())
		Expect(resourceObj.Status.Resource.Digest).To(Equal(descruntime.ConvertToV2Digest(&expected)))

		afterReconcile, err := repo.GetComponentVersion(ctx, componentName, "v1.0.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(*afterReconcile.Component.Resources[0].Digest).To(Equal(expected))
	})

	// Component signature verification runs before the resource-digest prerequisite and is not skipped
	// by Never. These cases put the signing config directly on the Resource so a mocked ready Component
	// cannot mask it.
	DescribeTable("signature verification with incomplete digests",
		func(ctx SpecContext, requestSignature, signCorrectly, verifyWithWrongKey, recordDigest bool, policy v1alpha1.VerificationPolicy, ready bool, reason, message string) {
			name := test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentName := "ocm.software/sig-digest-" + name
			namespace := test.NamespaceForTest(ctx)
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			ctfPath := GinkgoT().TempDir()
			fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
			Expect(err).NotTo(HaveOccurred())
			repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))), oci.WithTempDir(GinkgoT().TempDir()))
			Expect(err).NotTo(HaveOccurred())

			res := &descruntime.Resource{
				ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "data", Version: "v1.0.0"}},
				Type:        "plainText",
				Relation:    descruntime.LocalRelation,
				Access: &v2.LocalBlob{
					Type:      runtime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
					MediaType: "text/plain",
				},
			}
			stored, err := repo.AddLocalResource(ctx, componentName, "v1.0.0", res, inmemory.New(bytes.NewReader([]byte("content"))))
			Expect(err).NotTo(HaveOccurred())
			if !recordDigest {
				stored.Digest = nil
			}
			Expect(repo.AddComponentVersion(ctx, &descruntime.Descriptor{
				Meta: descruntime.Meta{Version: "v2"},
				Component: descruntime.Component{
					ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: "v1.0.0"}},
					Provider:      descruntime.Provider{Name: "ocm.software"},
					Resources:     []descruntime.Resource{*stored},
				},
			})).To(Succeed())

			signatureName := "test-signature"
			var ocmConfigs []v1alpha1.OCMConfiguration
			if signCorrectly {
				desc, err := repo.GetComponentVersion(ctx, componentName, "v1.0.0")
				Expect(err).ToNot(HaveOccurred())
				normalised, err := normalisation.Normalise(desc, v4alpha1.Algorithm)
				Expect(err).ToNot(HaveOccurred())
				signature, pubKey := test.SignComponent(ctx, signatureName, signingv1alpha1.AlgorithmRSASSAPSS, normalised, pm)
				desc.Signatures = append(desc.Signatures, signature)
				Expect(repo.AddComponentVersion(ctx, desc)).To(Succeed())

				verifyKey := pubKey
				if verifyWithWrongKey {
					_, verifyKey = test.SignComponent(ctx, signatureName, signingv1alpha1.AlgorithmRSASSAPSS, normalised, pm)
				}
				configSecret := test.SetupSignatureVerificationConfig(ctx, k8sClient, namespace.GetName(), "signature-verification-"+name,
					test.SignatureVerification{Signature: signatureName, Algorithm: signingv1alpha1.AlgorithmRSASSAPSS, PublicKey: verifyKey})
				ocmConfigs = []v1alpha1.OCMConfiguration{test.SecretOCMConfiguration(configSecret)}
			} else if requestSignature {
				// Request a signature that was never produced: only the verification config, no signature in the CV.
				_, pubKey := test.SignComponent(ctx, signatureName, signingv1alpha1.AlgorithmRSASSAPSS, []byte("unused"), pm)
				configSecret := test.SetupSignatureVerificationConfig(ctx, k8sClient, namespace.GetName(), "signature-verification-"+name,
					test.SignatureVerification{Signature: signatureName, Algorithm: signingv1alpha1.AlgorithmRSASSAPSS, PublicKey: pubKey})
				ocmConfigs = []v1alpha1.OCMConfiguration{test.SecretOCMConfiguration(configSecret)}
			}

			specData, err := json.Marshal(&ctfv1.Repository{
				Type:       runtime.Type{Name: "ctf", Version: "v1"},
				FilePath:   ctfPath,
				AccessMode: ctfv1.AccessModeReadOnly,
			})
			Expect(err).NotTo(HaveOccurred())

			componentObj := test.MockComponent(ctx, "component-"+name, namespace.GetName(), &test.MockComponentOptions{
				Client:   k8sClient,
				Recorder: recorder,
				Info: v1alpha1.ComponentInfo{
					Component:      componentName,
					Version:        "v1.0.0",
					RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
				},
				Repository: "ocm.software/test-repository",
			})
			DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, componentObj) })

			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{Name: "resource-" + name, Namespace: namespace.GetName()},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef:       corev1.LocalObjectReference{Name: componentObj.GetName()},
					VerificationPolicy: policy,
					OCMConfig:          ocmConfigs,
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{Resource: runtime.Identity{"name": "data"}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, resourceObj) })

			if ready {
				test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{"Status.Resource.Name": "data"})
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObj)).To(Succeed())
				Expect(resourceObj.Status.Resource).NotTo(BeNil())
				Expect(resourceObj.Status.Resource.Digest).To(BeNil())
				Expect(resourceObj.Status.ObservedGeneration).To(Equal(resourceObj.GetGeneration()))
				return
			}
			test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, resourceObj, reason, message)
		},
		// requestSignature, signCorrectly, verifyWithWrongKey, recordDigest, policy, ready, reason, message
		Entry("required signature absent under Always", true, false, false, false, v1alpha1.VerificationPolicyAlways, false,
			v1alpha1.GetComponentVersionFailedReason, "signature test-signature not found"),
		Entry("required signature absent under Never", true, false, false, false, v1alpha1.VerificationPolicyNever, false,
			v1alpha1.GetComponentVersionFailedReason, "signature test-signature not found"),
		Entry("wrong verification key under Always", false, true, true, false, v1alpha1.VerificationPolicyAlways, false,
			v1alpha1.GetComponentVersionFailedReason, "signature verification failed for signature test-signature"),
		Entry("valid signature but missing resource digest under Always", false, true, false, false, v1alpha1.VerificationPolicyAlways, false,
			v1alpha1.GetOCMResourceFailedReason, "has no digest to verify"),
		Entry("valid signature and missing resource digest under Never", false, true, false, false, v1alpha1.VerificationPolicyNever, true,
			"", ""),
	)

	// A Resource stuck on a missing digest recovers when the Component resolves a newer version that
	// records one, through the existing Component watch — not only by disabling verification.
	It("recovers when the Component resolves a version with a recorded digest", func(ctx SpecContext) {
		componentName := "ocm.software/recover-digest"
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "recover-digest"}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		ctfPath := GinkgoT().TempDir()
		fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
		Expect(err).NotTo(HaveOccurred())
		repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))), oci.WithTempDir(GinkgoT().TempDir()))
		Expect(err).NotTo(HaveOccurred())

		storeVersion := func(version string, keepDigest bool) descruntime.Digest {
			res := &descruntime.Resource{
				ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "data", Version: version}},
				Type:        "plainText",
				Relation:    descruntime.LocalRelation,
				Access: &v2.LocalBlob{
					Type:      runtime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
					MediaType: "text/plain",
				},
			}
			stored, err := repo.AddLocalResource(ctx, componentName, version, res, inmemory.New(bytes.NewReader([]byte("content"))))
			Expect(err).NotTo(HaveOccurred())
			recorded := *stored.Digest
			if !keepDigest {
				stored.Digest = nil
			}
			Expect(repo.AddComponentVersion(ctx, &descruntime.Descriptor{
				Meta: descruntime.Meta{Version: "v2"},
				Component: descruntime.Component{
					ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: version}},
					Provider:      descruntime.Provider{Name: "ocm.software"},
					Resources:     []descruntime.Resource{*stored},
				},
			})).To(Succeed())
			return recorded
		}
		Expect(storeVersion("v1.0.0", false)).NotTo(Equal(descruntime.Digest{}))
		recoveredDigest := storeVersion("v1.0.1", true)
		v100, err := repo.GetComponentVersion(ctx, componentName, "v1.0.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(v100.Component.Resources[0].Digest).To(BeNil())
		v101, err := repo.GetComponentVersion(ctx, componentName, "v1.0.1")
		Expect(err).NotTo(HaveOccurred())
		Expect(*v101.Component.Resources[0].Digest).To(Equal(recoveredDigest))
		specData, err := json.Marshal(&ctfv1.Repository{
			Type:       runtime.Type{Name: "ctf", Version: "v1"},
			FilePath:   ctfPath,
			AccessMode: ctfv1.AccessModeReadOnly,
		})
		Expect(err).NotTo(HaveOccurred())

		componentObj := test.MockComponent(ctx, "component-recover", namespace.GetName(), &test.MockComponentOptions{
			Client:   k8sClient,
			Recorder: recorder,
			Info: v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        "v1.0.0",
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			},
			Repository: "ocm.software/test-repository",
		})
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, componentObj) })

		resourceObj := &v1alpha1.Resource{
			ObjectMeta: metav1.ObjectMeta{Name: "resource-recover", Namespace: namespace.GetName()},
			Spec: v1alpha1.ResourceSpec{
				ComponentRef: corev1.LocalObjectReference{Name: componentObj.GetName()},
				Resource: v1alpha1.ResourceID{
					ByReference: v1alpha1.ResourceReference{Resource: runtime.Identity{"name": "data"}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, resourceObj) })

		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, resourceObj, v1alpha1.GetOCMResourceFailedReason, "has no digest to verify")

		generationBeforeRecovery := resourceObj.GetGeneration()
		By("resolving the newer component version that records a digest")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(componentObj), componentObj)).To(Succeed())
		old := componentObj.DeepCopy()
		componentObj.Status.Component.Version = "v1.0.1"
		Expect(k8sClient.Status().Patch(ctx, componentObj, client.MergeFrom(old))).To(Succeed())

		test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{
			"Status.Resource.Name":     "data",
			"Status.Component.Version": "v1.0.1",
		})
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObj)).To(Succeed())
		Expect(resourceObj.Status.Resource.Digest).To(Equal(descruntime.ConvertToV2Digest(&recoveredDigest)))
		Expect(resourceObj.GetGeneration()).To(Equal(generationBeforeRecovery))
		Expect(resourceObj.Status.ObservedGeneration).To(Equal(resourceObj.GetGeneration()))
	})

	// The Always policy rejects a missing selected-resource digest before any content is fetched,
	// and the decision tracks the current policy across spec changes.
	It("enforces digest policy before external requests and across policy changes", func(ctx SpecContext) {
		componentName := "ocm.software/verification-findings/external-policy"
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "verification-findings-"}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		var requests atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			_, _ = w.Write([]byte("content"))
		}))
		DeferCleanup(server.Close)

		repo, specData := test.SetupCTFComponentVersionRepository(ctx, GinkgoT().TempDir(), nil)
		Expect(repo.AddComponentVersion(ctx, &descruntime.Descriptor{
			Meta: descruntime.Meta{Version: "v2"},
			Component: descruntime.Component{
				ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: "v1.0.0"}},
				Provider:      descruntime.Provider{Name: "ocm.software"},
				Resources: []descruntime.Resource{{
					ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "data", Version: "v1.0.0"}},
					Type:        "blob",
					Relation:    descruntime.ExternalRelation,
					Access: &runtime.Raw{
						Type: runtime.NewVersionedType("wget", "v1"),
						Data: []byte(`{"type":"wget/v1","url":"` + server.URL + `/data"}`),
					},
				}},
			},
		})).To(Succeed())

		readBack, err := repo.GetComponentVersion(ctx, componentName, "v1.0.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(readBack.Component.Resources[0].Digest).To(BeNil())

		componentObj := test.MockComponent(ctx, "component", namespace.GetName(), &test.MockComponentOptions{
			Client:   k8sClient,
			Recorder: recorder,
			Info: v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        "v1.0.0",
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			},
			Repository: "ocm.software/test-repository",
		})
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, componentObj) })

		resourceObj := &v1alpha1.Resource{
			ObjectMeta: metav1.ObjectMeta{Name: "resource", Namespace: namespace.GetName()},
			Spec: v1alpha1.ResourceSpec{
				ComponentRef:       corev1.LocalObjectReference{Name: componentObj.GetName()},
				VerificationPolicy: v1alpha1.VerificationPolicyAlways,
				Resource:           v1alpha1.ResourceID{ByReference: v1alpha1.ResourceReference{Resource: runtime.Identity{"name": "data"}}},
			},
		}
		Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, resourceObj) })

		By("rejecting the missing digest under Always without fetching the content")
		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, resourceObj, v1alpha1.GetOCMResourceFailedReason, "has no digest to verify")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObj)).To(Succeed())
		Expect(resourceObj.Status.Resource).To(BeNil())
		Expect(requests.Load()).To(BeZero())

		By("opting out with Never")
		old := resourceObj.DeepCopy()
		resourceObj.Spec.VerificationPolicy = v1alpha1.VerificationPolicyNever
		Expect(k8sClient.Patch(ctx, resourceObj, client.MergeFrom(old))).To(Succeed())
		test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{"Status.Resource.Name": "data"})
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObj)).To(Succeed())
		Expect(resourceObj.Status.Resource).NotTo(BeNil())
		Expect(resourceObj.Status.Resource.Digest).To(BeNil())
		Expect(resourceObj.Status.ObservedGeneration).To(Equal(resourceObj.GetGeneration()))
		Expect(requests.Load()).To(BeZero())

		By("re-enabling Always")
		old = resourceObj.DeepCopy()
		resourceObj.Spec.VerificationPolicy = v1alpha1.VerificationPolicyAlways
		Expect(k8sClient.Patch(ctx, resourceObj, client.MergeFrom(old))).To(Succeed())
		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, resourceObj, v1alpha1.GetOCMResourceFailedReason, "has no digest to verify")
		Consistently(requests.Load).WithContext(ctx).WithTimeout(time.Second).WithPolling(50 * time.Millisecond).Should(BeZero())
	})

	// Resource Always applies only to the selected resource: a signed component with an undigested,
	// unselected sibling verifies and is never fetched.
	It("verifies a signed resource without fetching its undigested sibling", func(ctx SpecContext) {
		componentName := "ocm.software/verification-findings/signed-sibling"
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "verification-findings-"}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		var otherRequests atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			otherRequests.Add(1)
			_, _ = w.Write([]byte("content"))
		}))
		DeferCleanup(server.Close)

		ctfPath := GinkgoT().TempDir()
		fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
		Expect(err).NotTo(HaveOccurred())
		repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))), oci.WithTempDir(GinkgoT().TempDir()))
		Expect(err).NotTo(HaveOccurred())

		res := &descruntime.Resource{
			ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "data", Version: "v1.0.0"}},
			Type:        "plainText",
			Relation:    descruntime.LocalRelation,
			Access: &v2.LocalBlob{
				Type:      runtime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
				MediaType: "text/plain",
			},
		}
		stored, err := repo.AddLocalResource(ctx, componentName, "v1.0.0", res, inmemory.New(bytes.NewReader([]byte("content"))))
		Expect(err).NotTo(HaveOccurred())
		recordedDigest := *stored.Digest
		other := descruntime.Resource{
			ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "other", Version: "v1.0.0"}},
			Type:        "blob",
			Relation:    descruntime.ExternalRelation,
			Access: &runtime.Raw{
				Type: runtime.NewVersionedType("wget", "v1"),
				Data: []byte(`{"type":"wget/v1","url":"` + server.URL + `/other"}`),
			},
		}
		desc := &descruntime.Descriptor{
			Meta: descruntime.Meta{Version: "v2"},
			Component: descruntime.Component{
				ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: "v1.0.0"}},
				Provider:      descruntime.Provider{Name: "ocm.software"},
				Resources:     []descruntime.Resource{*stored, other},
			},
		}
		Expect(repo.AddComponentVersion(ctx, desc)).To(Succeed())

		readBack, err := repo.GetComponentVersion(ctx, componentName, "v1.0.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(*readBack.Component.Resources[0].Digest).To(Equal(recordedDigest))
		Expect(readBack.Component.Resources[1].Digest).To(BeNil())

		normalised, err := normalisation.Normalise(readBack, v4alpha1.Algorithm)
		Expect(err).NotTo(HaveOccurred())
		signature, pubKey := test.SignComponent(ctx, "test-signature", signingv1alpha1.AlgorithmRSASSAPSS, normalised, pm)
		readBack.Signatures = append(readBack.Signatures, signature)
		Expect(repo.AddComponentVersion(ctx, readBack)).To(Succeed())

		specData, err := json.Marshal(&ctfv1.Repository{Type: runtime.Type{Name: "ctf", Version: "v1"}, FilePath: ctfPath, AccessMode: ctfv1.AccessModeReadOnly})
		Expect(err).NotTo(HaveOccurred())
		configSecret := test.SetupSignatureVerificationConfig(ctx, k8sClient, namespace.GetName(), "signature-verification",
			test.SignatureVerification{Signature: "test-signature", Algorithm: signingv1alpha1.AlgorithmRSASSAPSS, PublicKey: pubKey})
		resourceConfig := test.SecretOCMConfiguration(configSecret)
		resourceConfig.Policy = v1alpha1.ConfigurationPolicyDoNotPropagate

		componentObj := test.MockComponent(ctx, "component", namespace.GetName(), &test.MockComponentOptions{
			Client:   k8sClient,
			Recorder: recorder,
			Info: v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        "v1.0.0",
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			},
			Repository: "ocm.software/test-repository",
		})
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, componentObj) })

		resourceObj := &v1alpha1.Resource{
			ObjectMeta: metav1.ObjectMeta{Name: "resource", Namespace: namespace.GetName()},
			Spec: v1alpha1.ResourceSpec{
				ComponentRef:       corev1.LocalObjectReference{Name: componentObj.GetName()},
				VerificationPolicy: v1alpha1.VerificationPolicyAlways,
				OCMConfig:          []v1alpha1.OCMConfiguration{resourceConfig},
				Resource:           v1alpha1.ResourceID{ByReference: v1alpha1.ResourceReference{Resource: runtime.Identity{"name": "data"}}},
			},
		}
		Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, resourceObj) })

		test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{"Status.Resource.Name": "data"})
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObj)).To(Succeed())
		Expect(resourceObj.Status.Resource.Digest).To(Equal(descruntime.ConvertToV2Digest(&recordedDigest)))
		Expect(resourceObj.Status.ObservedGeneration).To(Equal(resourceObj.GetGeneration()))

		afterReconcile, err := repo.GetComponentVersion(ctx, componentName, "v1.0.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(*afterReconcile.Component.Resources[0].Digest).To(Equal(recordedDigest))
		Expect(afterReconcile.Component.Resources[1].Digest).To(BeNil())
		Consistently(otherRequests.Load).WithContext(ctx).WithTimeout(time.Second).WithPolling(50 * time.Millisecond).Should(BeZero())
	})

	// Without a digest processor for its access type (here a type no plugin provides) a
	// resource cannot be verified, so it fails unless verification is skipped.
	It("fails a resource that cannot be verified", func(ctx SpecContext) {
		componentName := "ocm.software/unverifiable"
		namespace := test.NamespaceForTest(ctx)
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		_, specData := test.SetupCTFComponentVersionRepository(ctx, GinkgoT().TempDir(), []*descruntime.Descriptor{{
			Meta: descruntime.Meta{Version: "v2"},
			Component: descruntime.Component{
				ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: "v1.0.0"}},
				Provider:      descruntime.Provider{Name: "ocm.software"},
				Resources: []descruntime.Resource{{
					ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "source", Version: "v1.0.0"}},
					Type:        "blob",
					Relation:    descruntime.ExternalRelation,
					Access: &runtime.Raw{
						Type: runtime.Type{Name: "unknownAccess", Version: "v1"},
						Data: []byte(`{"type":"unknownAccess/v1"}`),
					},
					Digest: &descruntime.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: descruntime.GenericBlobDigestV1,
						Value:                  digest.FromString("content").Encoded(),
					},
				}},
			},
		}})

		componentObj := test.MockComponent(ctx, "unverifiable", namespace.GetName(), &test.MockComponentOptions{
			Client:   k8sClient,
			Recorder: recorder,
			Info: v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        "v1.0.0",
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			},
			Repository: "ocm.software/test-repository",
		})
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, componentObj) })

		resourceObj := &v1alpha1.Resource{
			ObjectMeta: metav1.ObjectMeta{Name: "unverifiable", Namespace: namespace.GetName()},
			Spec: v1alpha1.ResourceSpec{
				ComponentRef: corev1.LocalObjectReference{Name: componentObj.GetName()},
				Resource: v1alpha1.ResourceID{
					ByReference: v1alpha1.ResourceReference{Resource: runtime.Identity{"name": "source"}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, resourceObj) })

		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, resourceObj, v1alpha1.GetOCMResourceFailedReason, "cannot be verified")
	})
})
