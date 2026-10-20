package cmd_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitorus/timestamp"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/cli/cmd/internal/test"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/signing/tsa"
)

// newMockTSA starts a local RFC 3161 TSA that attests genTime and counts its
// requests. Its signer is a self-signed certificate with the critical
// id-kp-timeStamping EKU, valid long enough before now to cover backdated
// generation times.
func newMockTSA(t *testing.T, genTime time.Time) (*httptest.Server, *x509.Certificate, *atomic.Int64) {
	t.Helper()
	r := require.New(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)
	ekuVal, err := asn1.Marshal([]asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 8}})
	r.NoError(err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "mock TSA"},
		NotBefore:             time.Now().Add(-100 * time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		ExtraExtensions:       []pkix.Extension{{Id: asn1.ObjectIdentifier{2, 5, 29, 37}, Critical: true, Value: ekuVal}},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	r.NoError(err)
	cert, err := x509.ParseCertificate(der)
	r.NoError(err)

	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		body, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		tsReq, err := timestamp.ParseRequest(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		out, err := (&timestamp.Timestamp{
			HashAlgorithm:     tsReq.HashAlgorithm,
			HashedMessage:     tsReq.HashedMessage,
			Time:              genTime.UTC().Truncate(time.Second),
			Nonce:             tsReq.Nonce,
			Policy:            asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0},
			AddTSACertificate: true,
		}).CreateResponseWithOpts(cert, key, crypto.SHA256)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/timestamp-reply")
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)
	return srv, cert, &requests
}

// addTSATestComponentVersion constructs a component version in a fresh CTF and
// returns its reference.
func addTSATestComponentVersion(t *testing.T, name string) string {
	t.Helper()
	r := require.New(t)
	tmp := t.TempDir()
	constructor := filepath.Join(tmp, "component-constructor.yaml")
	r.NoError(os.WriteFile(constructor, []byte(fmt.Sprintf(`
name: %s
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: data
    type: blob
    input:
      type: utf8/v1
      text: "tsa test"
`, name)), 0o600))
	archive := filepath.Join(tmp, "transport-archive")
	_, err := test.OCM(t, test.WithArgs("add", "cv", "--constructor", constructor, "--repository", archive))
	r.NoError(err)
	return archive + "//" + name + ":1.0.0"
}

// getTSATestComponentVersion opens the CTF behind ref and returns its repository
// and the descriptor of component name in version 1.0.0.
func getTSATestComponentVersion(t *testing.T, ref, name string) (*oci.Repository, *descriptor.Descriptor) {
	t.Helper()
	r := require.New(t)
	fs, err := filesystem.NewFS(strings.SplitN(ref, "//", 2)[0], os.O_RDWR)
	r.NoError(err)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	r.NoError(err)
	desc, err := repo.GetComponentVersion(t.Context(), name, "1.0.0")
	r.NoError(err)
	return repo, desc
}

// writeExpiredSigner writes a key and a self-signed certificate that expired a
// day ago but was valid two days ago.
func writeExpiredSigner(t *testing.T, dir string) (keyPath, chainPath string) {
	t.Helper()
	r := require.New(t)
	key := mustKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "expired signer"},
		NotBefore:             time.Now().Add(-72 * time.Hour),
		NotAfter:              time.Now().Add(-24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	r.NoError(err)
	cert, err := x509.ParseCertificate(der)
	r.NoError(err)
	return writeKeyAndChain(t, dir, key, cert)
}

func rsaConsumerYAML(signature, chainPath, keyPath, extraProperties string) string {
	return fmt.Sprintf(`
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: %s
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem_file: %s
        private_key_pem_file: %s%s`, signature, chainPath, keyPath, extraProperties)
}

func pemSigningConfigYAML(consumers string) string {
	return `type: generic.config.ocm.software/v1
configurations:
- type: signing.config.ocm.software/v1alpha1
  signer:
    type: RSASigningConfiguration/v1alpha1
    signatureEncodingPolicy: PEM
- type: credentials.config.ocm.software
  consumers:` + consumers + "\n"
}

func tsaConsumerYAML(identityAttributes, rootPath string) string {
	return `
  - identity:
      type: TSA/v1alpha1` + identityAttributes + `
    credentials:
    - type: TSACredentials/v1alpha1
      rootCertsPEMFile: ` + rootPath
}

func Test_Verify_TSA_Timestamp_Validates_Expired_Certificate(t *testing.T) {
	// The TSA attests a signing time at which the now expired certificate was valid.
	tsaSrv, tsaCert, _ := newMockTSA(t, time.Now().Add(-48*time.Hour))
	tsaURL, err := url.Parse(tsaSrv.URL)
	require.NoError(t, err)

	dir := t.TempDir()
	keyPath, chainPath := writeExpiredSigner(t, dir)
	tsaRoot := writeCertsPEM(t, dir, "tsa-root.pem", tsaCert)
	rsaConsumer := rsaConsumerYAML("default", chainPath, keyPath, "")

	writeConfig := func(name, consumers string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(pemSigningConfigYAML(consumers)), 0o600))
		return p
	}
	withoutTSA := writeConfig("no-tsa.yaml", rsaConsumer)
	genericRoots := writeConfig("generic.yaml", rsaConsumer+tsaConsumerYAML("", tsaRoot))

	// legacyTimestamp attaches what the legacy OCM CLI writes: a token over the
	// descriptor digest, stored as a bare SignedData under "TIMESTAMP INFO".
	legacyTimestamp := func(t *testing.T, sig *descriptor.Signature) {
		t.Helper()
		r := require.New(t)
		digest, err := hex.DecodeString(sig.Digest.Value)
		r.NoError(err)
		token, err := tsa.RequestTimestamp(t.Context(), nil, tsaSrv.URL, crypto.SHA256, digest)
		r.NoError(err)
		var contentInfo struct {
			ContentType asn1.ObjectIdentifier
			Content     asn1.RawValue `asn1:"explicit,tag:0"`
		}
		_, err = asn1.Unmarshal(token.Raw, &contentInfo)
		r.NoError(err)
		sig.Timestamp = &descriptor.TimestampSpec{
			Value: string(pem.EncodeToMemory(&pem.Block{Type: "TIMESTAMP INFO", Bytes: contentInfo.Content.Bytes})),
			Time:  descriptor.CreationTime(token.Time),
		}
	}

	tests := []struct {
		name     string
		signArgs []string
		// timestamp, when set, attaches a timestamp to the persisted signature.
		timestamp    func(t *testing.T, sig *descriptor.Signature)
		verifyConfig string
		verifyArgs   []string
		// wantErr, when set, is expected from verifying with verifyConfig.
		wantErr string
	}{
		{
			name:         "URL-specific TSA identity",
			signArgs:     []string{"--tsa-url", tsaSrv.URL},
			verifyConfig: writeConfig("url.yaml", rsaConsumer+tsaConsumerYAML(fmt.Sprintf("\n      hostname: %q\n      port: %q\n      scheme: http", tsaURL.Hostname(), tsaURL.Port()), tsaRoot)),
		},
		{name: "generic TSA identity", signArgs: []string{"--tsa-url", tsaSrv.URL}, verifyConfig: genericRoots},
		{
			name:         "legacy OCM timestamp over the descriptor digest",
			timestamp:    legacyTimestamp,
			verifyConfig: genericRoots,
			verifyArgs:   []string{"--insecure-trust-legacy-timestamps"},
		},
		{
			// A legacy token covers only the descriptor digest, which is known
			// before signing, so its time is not used without the opt-in.
			name:         "legacy OCM timestamp is not trusted without opt-in",
			timestamp:    legacyTimestamp,
			verifyConfig: genericRoots,
			wantErr:      "certificate has expired",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			name := "ocm.software/tsa-expired-cert"
			ref := addTSATestComponentVersion(t, name)
			_, err := test.OCM(t, test.WithArgs(append([]string{"sign", "cv", ref, "--config", withoutTSA}, tc.signArgs...)...))
			r.NoError(err)
			if tc.timestamp != nil {
				repo, desc := getTSATestComponentVersion(t, ref, name)
				r.Len(desc.Signatures, 1)
				tc.timestamp(t, &desc.Signatures[0])
				r.NoError(repo.AddComponentVersion(t.Context(), desc))
			}

			_, err = test.OCM(t, test.WithArgs("verify", "cv", ref, "--config", withoutTSA))
			r.ErrorContains(err, "certificate has expired", "without TSA roots the expired certificate must be rejected")

			_, err = test.OCM(t, test.WithArgs(append([]string{"verify", "cv", ref, "--config", tc.verifyConfig}, tc.verifyArgs...)...))
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err, "a trusted timestamp must validate the certificate as of the signing time")
		})
	}
}

// Test_Sign_TSA_Guards covers the checks that must hold before or around the
// TSA request, asserting how often the TSA was contacted.
func Test_Sign_TSA_Guards(t *testing.T) {
	t.Setenv("SIGSTORE_ID_TOKEN", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")
	tsaSrv, _, tsaRequests := newMockTSA(t, time.Now())

	dir := t.TempDir()
	key := mustKey(t)
	keyPath, chainPath := writeKeyAndChain(t, dir, key, mustSelfSigned(t, "signer", key))
	rsaConsumers := rsaConsumerYAML("default", chainPath, keyPath, "") + rsaConsumerYAML("other", chainPath, keyPath, "")
	writeConfig := func(name, body string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
		return p
	}
	config := writeConfig("rsa.yaml", pemSigningConfigYAML(rsaConsumers))
	wrongRoot := writeConfig("wrong-root.yaml", pemSigningConfigYAML(rsaConsumers+tsaConsumerYAML("", writeCertsPEM(t, dir, "wrong-root.pem", mustSelfSigned(t, "wrong root", mustKey(t))))))
	sigstore := writeConfig("sigstore.yaml", `type: generic.config.ocm.software/v1
configurations:
- type: signing.config.ocm.software/v1alpha1
  signer:
    type: SigstoreSigningConfiguration/v1alpha1
`)
	ocm := func(t *testing.T, args ...string) error {
		t.Helper()
		_, err := test.OCM(t, test.WithArgs(args...))
		return err
	}

	tests := []struct {
		name            string
		run             func(t *testing.T, ref string) error
		wantErr         string
		wantTSARequests int64
	}{
		{
			// Sigstore timestamps its bundles with TSAs from its own signing config.
			name: "Sigstore signer is rejected",
			run: func(t *testing.T, ref string) error {
				t.Helper()
				return ocm(t, "sign", "cv", ref, "--config", sigstore, "--tsa-url", tsaSrv.URL)
			},
			wantErr: "cannot be used with a Sigstore signer",
		},
		{
			name: "dry run neither contacts the TSA nor persists",
			run: func(t *testing.T, ref string) error {
				t.Helper()
				if err := ocm(t, "sign", "cv", ref, "--config", config, "--tsa-url", tsaSrv.URL, "--dry-run"); err != nil {
					return err
				}
				_, desc := getTSATestComponentVersion(t, ref, "ocm.software/tsa-guards")
				require.Empty(t, desc.Signatures)
				return nil
			},
		},
		{
			name: "timestamping a second signature keeps the first one valid",
			run: func(t *testing.T, ref string) error {
				t.Helper()
				if err := ocm(t, "sign", "cv", ref, "--config", config, "--signature", "other", "--tsa-url", tsaSrv.URL); err != nil {
					return err
				}
				if err := ocm(t, "sign", "cv", ref, "--config", config, "--tsa-url", tsaSrv.URL); err != nil {
					return err
				}
				return ocm(t, "verify", "cv", ref, "--config", config)
			},
			wantTSARequests: 2,
		},
		{
			name: "wrong TSA root fails verification",
			run: func(t *testing.T, ref string) error {
				t.Helper()
				if err := ocm(t, "sign", "cv", ref, "--config", config, "--tsa-url", tsaSrv.URL); err != nil {
					return err
				}
				return ocm(t, "verify", "cv", ref, "--config", wrongRoot)
			},
			wantErr:         "TSA timestamp verification failed",
			wantTSARequests: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ref := addTSATestComponentVersion(t, "ocm.software/tsa-guards")
			before := tsaRequests.Load()
			err := tc.run(t, ref)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
			} else {
				r.NoError(err)
			}
			r.Equal(tc.wantTSARequests, tsaRequests.Load()-before)
		})
	}
}

// Signing with --tsa must not change the descriptor digest, so a digest that a
// parent pinned for the component at construction time stays valid.
func Test_Sign_TSA_Keeps_Descriptor_Digest(t *testing.T) {
	r := require.New(t)
	tsaSrv, _, _ := newMockTSA(t, time.Now())
	dir := t.TempDir()
	key := mustKey(t)
	keyPath, chainPath := writeKeyAndChain(t, dir, key, mustSelfSigned(t, "signer", key))
	config := filepath.Join(dir, "config.yaml")
	r.NoError(os.WriteFile(config, []byte(pemSigningConfigYAML(rsaConsumerYAML("default", chainPath, keyPath, ""))), 0o600))

	constructor := filepath.Join(dir, "component-constructor.yaml")
	r.NoError(os.WriteFile(constructor, []byte(`
components:
  - name: ocm.software/tsa-child
    version: 1.0.0
    provider:
      name: ocm.software
    resources:
      - name: data
        type: blob
        input:
          type: utf8/v1
          text: "tsa test"
  - name: ocm.software/tsa-parent
    version: 1.0.0
    provider:
      name: ocm.software
    componentReferences:
      - name: child
        componentName: ocm.software/tsa-child
        version: 1.0.0
`), 0o600))
	archive := filepath.Join(dir, "transport-archive")
	_, err := test.OCM(t, test.WithArgs("add", "cv", "--constructor", constructor, "--repository", archive))
	r.NoError(err)
	_, parent := getTSATestComponentVersion(t, archive+"//ocm.software/tsa-parent:1.0.0", "ocm.software/tsa-parent")
	r.Len(parent.Component.References, 1)
	pinned := parent.Component.References[0].Digest

	childRef := archive + "//ocm.software/tsa-child:1.0.0"
	_, err = test.OCM(t, test.WithArgs("sign", "cv", childRef, "--config", config, "--tsa-url", tsaSrv.URL))
	r.NoError(err)

	_, child := getTSATestComponentVersion(t, childRef, "ocm.software/tsa-child")
	r.Len(child.Signatures, 1)
	r.NotNil(child.Signatures[0].Timestamp)
	labelIdx := slices.IndexFunc(child.Component.Labels, func(l descriptor.Label) bool { return l.Name == tsa.TSAURLLabelPrefix+"default" })
	r.GreaterOrEqual(labelIdx, 0, "the TSA URL label must be recorded")
	r.False(child.Component.Labels[labelIdx].Signing, "the TSA URL label must not be signing-relevant")
	r.Equal(pinned.Value, child.Signatures[0].Digest.Value, "the timestamped signature must cover the digest the parent pinned")
}
