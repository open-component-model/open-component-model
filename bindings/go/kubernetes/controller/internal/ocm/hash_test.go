package ocm_test

import (
	"encoding/hex"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
)

var _ = Describe("Object Data Hashing", func() {
	Describe("HashMap", func() {
		It("returns a 32-byte SHA-256 digest", func() {
			digest, err := ocm.HashMap(map[string][]byte{"k": []byte("v")})
			Expect(err).NotTo(HaveOccurred())
			Expect(digest).To(HaveLen(32))
		})

		It("is deterministic for the same input", func() {
			m := map[string][]byte{"alpha": []byte("1"), "beta": []byte("2")}
			d1, err := ocm.HashMap(m)
			Expect(err).NotTo(HaveOccurred())
			d2, err := ocm.HashMap(m)
			Expect(err).NotTo(HaveOccurred())
			Expect(d1).To(Equal(d2))
		})

		It("returns the same digest regardless of Go map iteration order", func() {
			// Run many times to exercise random map ordering.
			m := map[string][]byte{"z": []byte("1"), "a": []byte("2"), "m": []byte("3")}
			first, err := ocm.HashMap(m)
			Expect(err).NotTo(HaveOccurred())
			for range 50 {
				d, err := ocm.HashMap(m)
				Expect(err).NotTo(HaveOccurred())
				Expect(d).To(Equal(first))
			}
		})

		It("distinguishes different values for the same key", func() {
			d1, err := ocm.HashMap(map[string][]byte{"k": []byte("val1")})
			Expect(err).NotTo(HaveOccurred())
			d2, err := ocm.HashMap(map[string][]byte{"k": []byte("val2")})
			Expect(err).NotTo(HaveOccurred())
			Expect(d1).NotTo(Equal(d2))
		})

		It("distinguishes different keys for the same value", func() {
			d1, err := ocm.HashMap(map[string][]byte{"key1": []byte("v")})
			Expect(err).NotTo(HaveOccurred())
			d2, err := ocm.HashMap(map[string][]byte{"key2": []byte("v")})
			Expect(err).NotTo(HaveOccurred())
			Expect(d1).NotTo(Equal(d2))
		})

		It("distinguishes key/value boundary ambiguity via null separator", func() {
			// {"a": "bc"} vs {"ab": "c"} must differ because the 0x00
			// separator makes the byte streams a\x00bc\x00 vs ab\x00c\x00.
			d1, err := ocm.HashMap(map[string][]byte{"a": []byte("bc")})
			Expect(err).NotTo(HaveOccurred())
			d2, err := ocm.HashMap(map[string][]byte{"ab": []byte("c")})
			Expect(err).NotTo(HaveOccurred())
			Expect(d1).NotTo(Equal(d2))
		})

		It("produces a deterministic hash for an empty map", func() {
			d, err := ocm.HashMap(map[string][]byte{})
			Expect(err).NotTo(HaveOccurred())
			Expect(d).To(HaveLen(32))
			// SHA-256 of empty input is a well-known constant.
			Expect(hex.EncodeToString(d)).To(Equal(
				"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"))
		})
	})

	Describe("GetSecretMapDataHash", func() {
		It("returns a 32-byte digest for a populated secret", func() {
			s := &corev1.Secret{Data: map[string][]byte{"user": []byte("admin")}}
			d, err := ocm.GetSecretMapDataHash(s)
			Expect(err).NotTo(HaveOccurred())
			Expect(d).To(HaveLen(32))
		})

		It("hashes nil secret the same as empty data", func() {
			dNil, err := ocm.GetSecretMapDataHash(nil)
			Expect(err).NotTo(HaveOccurred())
			dEmpty, err := ocm.GetSecretMapDataHash(&corev1.Secret{})
			Expect(err).NotTo(HaveOccurred())
			Expect(dNil).To(Equal(dEmpty))
		})

		It("hashes empty Data the same as empty map", func() {
			s := &corev1.Secret{Data: map[string][]byte{}}
			dExplicit, err := ocm.GetSecretMapDataHash(s)
			Expect(err).NotTo(HaveOccurred())
			dDefault, err := ocm.GetSecretMapDataHash(&corev1.Secret{})
			Expect(err).NotTo(HaveOccurred())
			Expect(dExplicit).To(Equal(dDefault))
		})

		It("produces different hashes for different data", func() {
			s1 := &corev1.Secret{Data: map[string][]byte{"k": []byte("v1")}}
			s2 := &corev1.Secret{Data: map[string][]byte{"k": []byte("v2")}}
			d1, err := ocm.GetSecretMapDataHash(s1)
			Expect(err).NotTo(HaveOccurred())
			d2, err := ocm.GetSecretMapDataHash(s2)
			Expect(err).NotTo(HaveOccurred())
			Expect(d1).NotTo(Equal(d2))
		})
	})

	Describe("GetConfigMapDataHash", func() {
		It("hashes nil config map the same as empty", func() {
			dNil, err := ocm.GetConfigMapDataHash(nil)
			Expect(err).NotTo(HaveOccurred())
			dEmpty, err := ocm.GetConfigMapDataHash(&corev1.ConfigMap{})
			Expect(err).NotTo(HaveOccurred())
			Expect(dNil).To(Equal(dEmpty))
		})

		It("merges Data (string) and BinaryData into the same hash space", func() {
			cmData := &corev1.ConfigMap{
				Data: map[string]string{"k": "v"},
			}
			cmBinary := &corev1.ConfigMap{
				BinaryData: map[string][]byte{"k": []byte("v")},
			}
			d1, err := ocm.GetConfigMapDataHash(cmData)
			Expect(err).NotTo(HaveOccurred())
			d2, err := ocm.GetConfigMapDataHash(cmBinary)
			Expect(err).NotTo(HaveOccurred())
			Expect(d1).To(Equal(d2))
		})

		It("produces different hashes when Data differs", func() {
			cm1 := &corev1.ConfigMap{Data: map[string]string{"k": "a"}}
			cm2 := &corev1.ConfigMap{Data: map[string]string{"k": "b"}}
			d1, err := ocm.GetConfigMapDataHash(cm1)
			Expect(err).NotTo(HaveOccurred())
			d2, err := ocm.GetConfigMapDataHash(cm2)
			Expect(err).NotTo(HaveOccurred())
			Expect(d1).NotTo(Equal(d2))
		})

		It("treats empty Data+BinaryData the same as nil ConfigMap", func() {
			cm := &corev1.ConfigMap{
				Data:       map[string]string{},
				BinaryData: map[string][]byte{},
			}
			dExplicit, err := ocm.GetConfigMapDataHash(cm)
			Expect(err).NotTo(HaveOccurred())
			dNil, err := ocm.GetConfigMapDataHash(nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(dExplicit).To(Equal(dNil))
		})
	})

	Describe("GetObjectHash", func() {
		It("dispatches Secret correctly", func() {
			s := &corev1.Secret{Data: map[string][]byte{"x": []byte("y")}}
			d, err := ocm.GetObjectHash(s)
			Expect(err).NotTo(HaveOccurred())
			expected, err := ocm.GetSecretMapDataHash(s)
			Expect(err).NotTo(HaveOccurred())
			Expect(d).To(Equal(expected))
		})

		It("dispatches ConfigMap correctly", func() {
			cm := &corev1.ConfigMap{Data: map[string]string{"x": "y"}}
			d, err := ocm.GetObjectHash(cm)
			Expect(err).NotTo(HaveOccurred())
			expected, err := ocm.GetConfigMapDataHash(cm)
			Expect(err).NotTo(HaveOccurred())
			Expect(d).To(Equal(expected))
		})

		It("returns error for unsupported object type", func() {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p"}}
			_, err := ocm.GetObjectHash(pod)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported object type"))
		})
	})

	Describe("GetObjectDataHash", func() {
		secret := func(data map[string][]byte) *corev1.Secret {
			return &corev1.Secret{Data: data}
		}
		configMap := func(data map[string]string) *corev1.ConfigMap {
			return &corev1.ConfigMap{Data: data}
		}

		It("returns a 64-hex-char string", func() {
			h, err := ocm.GetObjectDataHash[client.Object](
				secret(map[string][]byte{"k": []byte("v")}),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(h).To(HaveLen(64))
			Expect(h).To(MatchRegexp("^[0-9a-f]{64}$"))
		})

		It("is order-independent for Secrets", func() {
			s1 := secret(map[string][]byte{"a": []byte("1")})
			s2 := secret(map[string][]byte{"b": []byte("2")})
			h1, err := ocm.GetObjectDataHash[client.Object](s1, s2)
			Expect(err).NotTo(HaveOccurred())
			h2, err := ocm.GetObjectDataHash[client.Object](s2, s1)
			Expect(err).NotTo(HaveOccurred())
			Expect(h1).To(Equal(h2))
		})

		It("is order-independent for ConfigMaps", func() {
			c1 := configMap(map[string]string{"x": "1"})
			c2 := configMap(map[string]string{"y": "2"})
			h1, err := ocm.GetObjectDataHash[client.Object](c1, c2)
			Expect(err).NotTo(HaveOccurred())
			h2, err := ocm.GetObjectDataHash[client.Object](c2, c1)
			Expect(err).NotTo(HaveOccurred())
			Expect(h1).To(Equal(h2))
		})

		It("changes when a data value changes", func() {
			s1 := secret(map[string][]byte{"k": []byte("v1")})
			s2 := secret(map[string][]byte{"k": []byte("v2")})
			h1, err := ocm.GetObjectDataHash[client.Object](s1)
			Expect(err).NotTo(HaveOccurred())
			h2, err := ocm.GetObjectDataHash[client.Object](s2)
			Expect(err).NotTo(HaveOccurred())
			Expect(h1).NotTo(Equal(h2))
		})

		It("changes when a key changes", func() {
			s1 := secret(map[string][]byte{"key1": []byte("v")})
			s2 := secret(map[string][]byte{"key2": []byte("v")})
			h1, err := ocm.GetObjectDataHash[client.Object](s1)
			Expect(err).NotTo(HaveOccurred())
			h2, err := ocm.GetObjectDataHash[client.Object](s2)
			Expect(err).NotTo(HaveOccurred())
			Expect(h1).NotTo(Equal(h2))
		})

		It("returns an error when given an unsupported type", func() {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p"}}
			_, err := ocm.GetObjectDataHash[client.Object](pod)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported object type"))
		})

		It("handles empty object list (produces 64-hex hash of nothing)", func() {
			h, err := ocm.GetObjectDataHash[*corev1.Secret]()
			Expect(err).NotTo(HaveOccurred())
			Expect(h).To(HaveLen(64))
			Expect(h).To(MatchRegexp("^[0-9a-f]{64}$"))
		})

		It("single object hash differs from aggregate of two identical objects", func() {
			s := secret(map[string][]byte{"a": []byte("b")})
			h1, err := ocm.GetObjectDataHash[client.Object](s)
			Expect(err).NotTo(HaveOccurred())
			h2, err := ocm.GetObjectDataHash[client.Object](s, s)
			Expect(err).NotTo(HaveOccurred())
			Expect(h1).NotTo(Equal(h2))
		})
	})
})
