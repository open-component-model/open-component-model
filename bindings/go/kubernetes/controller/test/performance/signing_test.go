package main

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/verification"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	rsacredentialsv1 "ocm.software/open-component-model/bindings/go/rsa/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/signing"
)

func TestLevels(t *testing.T) {
	r := require.New(t)
	ref := func(name string) []reference {
		return []reference{{Name: referenceName, Component: name, Version: initialVersion}}
	}
	cvs := []componentVersion{
		{Name: "root", Version: "1.0.1", References: ref("mid")},
		{Name: "root", Version: initialVersion, References: ref("mid")},
		{Name: "mid", Version: initialVersion, References: ref("leaf")},
		{Name: "leaf", Version: initialVersion},
		{Name: "alone", Version: initialVersion},
	}

	var names [][]string
	for _, level := range levels(cvs) {
		var l []string
		for _, cv := range level {
			l = append(l, cv.Name+":"+cv.Version)
		}
		names = append(names, l)
	}
	r.Equal([][]string{
		{"leaf:1.0.0", "alone:1.0.0"},
		{"mid:1.0.0"},
		{"root:1.0.1", "root:1.0.0"},
	}, names)
}

func TestPublisherSign(t *testing.T) {
	r := require.New(t)
	s, err := newSigner()
	r.NoError(err)
	p := &publisher{signer: s, digests: map[string]descriptor.Digest{}}

	desc := func(name string) *descriptor.Descriptor {
		return &descriptor.Descriptor{
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				Provider:      descriptor.Provider{Name: "ocm.software"},
				ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: name, Version: initialVersion}},
			},
		}
	}
	child := desc("child")
	r.NoError(p.sign(t.Context(), child))

	parent := desc("parent")
	dig, err := p.componentDigest("child", initialVersion)
	r.NoError(err)
	parent.Component.References = []descriptor.Reference{{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: referenceName, Version: initialVersion}},
		Component:   "child",
		Digest:      dig,
	}}
	r.NoError(p.sign(t.Context(), parent))

	// The controller checks a child against its reference digest by digesting the child again.
	again, err := signing.GenerateDigest(t.Context(), child, slog.Default(), dig.NormalisationAlgorithm, dig.HashAlgorithm)
	r.NoError(err)
	r.Equal(dig.Value, again.Value, "the signature on the child must not change its digest")

	r.Len(parent.Signatures, 1)
	sig := parent.Signatures[0]
	r.Equal(signatureName, sig.Name)
	r.NoError(signing.VerifyDigestMatchesDescriptor(t.Context(), parent, sig, slog.Default()))
	r.NoError(s.handler.Verify(t.Context(), sig, nil, &rsacredentialsv1.RSACredentials{
		Type: rsacredentialsv1.VersionedType, PublicKeyPEM: s.publicKeyPEM,
	}))
}

func TestPublisherSignRefusesMissingDigest(t *testing.T) {
	r := require.New(t)
	s, err := newSigner()
	r.NoError(err)
	p := &publisher{signer: s, digests: map[string]descriptor.Digest{}}

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider:      descriptor.Provider{Name: "ocm.software"},
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: "parent", Version: initialVersion}},
			References: []descriptor.Reference{{
				ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: referenceName, Version: initialVersion}},
				Component:   "child",
			}},
		},
	}
	r.ErrorContains(p.sign(t.Context(), desc), "missing digest in componentReference")
	r.Empty(desc.Signatures)

	_, err = p.componentDigest("child", initialVersion)
	r.ErrorContains(err, "was not published before its parent")
}

func TestVerificationConfig(t *testing.T) {
	r := require.New(t)
	raw, err := verificationConfig("-----BEGIN RSA PUBLIC KEY-----\nAAAA\n-----END RSA PUBLIC KEY-----\n")
	r.NoError(err)
	r.NotContains(raw, "privateKey")

	var cfg genericv1.Config
	r.NoError(genericv1.Scheme.Decode(strings.NewReader(raw), &cfg))
	verifications, err := verification.GetVerifications(&configuration.Configuration{Config: &cfg})
	r.NoError(err)
	r.Len(verifications, 1, "the controller must verify exactly the signature the benchmark writes")
	r.Equal(signatureName, verifications[0].Signature)
	r.Equal("RSASigningConfiguration/v1alpha1", verifications[0].Verifier.GetType().String())
}
