package internal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/helm/chartarchive"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	helmidentityv1 "ocm.software/open-component-model/bindings/go/helm/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	wgetcredsv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
	wgetidentityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

// chartResourceRepo serves the chart from DownloadResource and derives no source credential identity.
type chartResourceRepo struct {
	repository.ResourceRepository
	chart []byte
}

func (s *chartResourceRepo) GetResourceCredentialConsumerIdentity(context.Context, *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (s *chartResourceRepo) DownloadResource(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
	return inmemory.New(bytes.NewReader(s.chart), inmemory.WithSize(int64(len(s.chart)))), nil
}

// credentialsByType resolves credentials by the type attribute of the consumer identity.
type credentialsByType map[string]runtime.Typed

func (c credentialsByType) Resolve(_ context.Context, id runtime.Identity) (runtime.Typed, error) {
	if creds, ok := c[id[runtime.IdentityAttributeType]]; ok {
		return creds, nil
	}
	return nil, credentials.ErrNotFound
}

// localChartRepo serves the chart as a local resource and records the request.
type localChartRepo struct {
	repository.ComponentVersionRepository
	chart              []byte
	component, version string
	identity           runtime.Identity
}

func (l *localChartRepo) GetLocalResource(_ context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	l.component, l.version, l.identity = component, version, identity
	return inmemory.New(bytes.NewReader(l.chart)), nil, nil
}

type localChartRepoProvider struct {
	repository.ComponentVersionRepositoryProvider
	repo *localChartRepo
}

func (p *localChartRepoProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(context.Context, runtime.Typed) (runtime.Identity, error) {
	return nil, errors.New("no identity")
}

func (p *localChartRepoProvider) GetComponentVersionRepository(context.Context, runtime.Typed, runtime.Typed) (repository.ComponentVersionRepository, error) {
	return p.repo, nil
}

type artifactoryRequest struct {
	method, path, contentType string
	username, password        string
	basic                     bool
	authorization             string
	body                      []byte
}

// artifactoryServer records every request and answers 201 to PUT and 200 otherwise.
func artifactoryServer(t *testing.T) (*httptest.Server, func() []artifactoryRequest) {
	t.Helper()
	var mu sync.Mutex
	var requests []artifactoryRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req := artifactoryRequest{method: r.Method, path: r.URL.Path, contentType: r.Header.Get("Content-Type"), authorization: r.Header.Get("Authorization"), body: body}
		req.username, req.password, req.basic = r.BasicAuth()
		mu.Lock()
		requests = append(requests, req)
		mu.Unlock()
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusCreated)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []artifactoryRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]artifactoryRequest(nil), requests...)
	}
}

func TestJFrogHelmUpload_Transform(t *testing.T) {
	chartTGZ, err := os.ReadFile("../../helm/testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	sum := sha256.Sum256(chartTGZ)
	chartDigest := hex.EncodeToString(sum[:])

	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&JFrogHelmUploadTransformation{}, JFrogHelmUploadVersionedType)
	scheme.MustRegisterScheme(helmaccess.Scheme)

	source := func() *descriptorv2.Resource {
		return &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{Name: "renamed", Version: "9.9.9"},
			Type:        "helmChart",
			Relation:    descriptorv2.ExternalRelation,
			Access:      &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1","url":"https://charts.example/mychart-0.1.0.tgz"}`)},
		}
	}
	step := func(url string, reindex bool, res *descriptorv2.Resource) *JFrogHelmUploadTransformation {
		return &JFrogHelmUploadTransformation{
			Type: JFrogHelmUploadVersionedType,
			ID:   "upload",
			Spec: &JFrogHelmUploadSpec{Resource: res, URL: url, Repository: "helm-local", Reindex: reindex},
		}
	}
	transformer := func(creds credentials.Resolver) *JFrogHelmUpload {
		repo := &chartResourceRepo{chart: chartTGZ}
		return &JFrogHelmUpload{
			Scheme:             scheme,
			Charts:             &chartarchive.Source{ResourceRepository: repo},
			ResourceRepository: repo,
			CredentialProvider: creds,
		}
	}
	helmCreds := &helmcredsv1.HelmHTTPCredentials{Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), Username: "helm-user", Password: "helm-pass"}
	wgetCreds := &wgetcredsv1.WgetCredentials{Type: wgetcredsv1.WgetCredentialsVersionedType, IdentityToken: "wget-token"}
	helmType, wgetType := helmidentityv1.Type.String(), wgetidentityv1.Type.String()

	t.Run("uploads under the Chart.yaml name, reindexes and publishes Helm/v1", func(t *testing.T) {
		r := require.New(t)
		srv, requests := artifactoryServer(t)
		out, err := transformer(nil).Transform(t.Context(), step(srv.URL, true, source()))
		r.NoError(err)

		got := requests()
		r.Len(got, 2)
		r.Equal(http.MethodPut, got[0].method)
		r.Equal("/artifactory/helm-local/mychart-0.1.0.tgz", got[0].path)
		r.Equal("application/gzip", got[0].contentType)
		r.Equal(chartTGZ, got[0].body)
		r.Equal(http.MethodPost, got[1].method)
		r.Equal("/artifactory/api/helm/helm-local/reindex", got[1].path)

		res := out.(*JFrogHelmUploadTransformation).Output.Resource
		r.Equal("renamed", res.Name)
		var access helmaccessv1.Helm
		r.NoError(helmaccess.Scheme.Convert(res.Access, &access))
		r.Equal(srv.URL+"/artifactory/api/helm/helm-local", access.HelmRepository)
		r.Equal("mychart:0.1.0", access.HelmChart)
		r.Equal(&descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: chartDigest}, res.Digest)
	})

	t.Run("no reindex when disabled", func(t *testing.T) {
		r := require.New(t)
		srv, requests := artifactoryServer(t)
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, false, source()))
		r.NoError(err)
		got := requests()
		r.Len(got, 1)
		r.Equal(http.MethodPut, got[0].method)
	})

	credTests := []struct {
		name      string
		creds     credentialsByType
		wantBasic []string
		wantAuth  string
		wantErr   string
	}{
		{name: "HelmChartRepository HelmHTTPCredentials use basic auth", creds: credentialsByType{helmType: helmCreds}, wantBasic: []string{"helm-user", "helm-pass"}},
		{name: "Wget WgetCredentials use a bearer token", creds: credentialsByType{wgetType: wgetCreds}, wantAuth: "Bearer wget-token"},
		{name: "HelmChartRepository credentials win over Wget", creds: credentialsByType{helmType: helmCreds, wgetType: wgetCreds}, wantBasic: []string{"helm-user", "helm-pass"}},
		{name: "HelmHTTPCredentials client certificates are rejected", creds: credentialsByType{helmType: &helmcredsv1.HelmHTTPCredentials{
			Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), CertFile: "/cert.pem", KeyFile: "/key.pem",
		}}, wantErr: "HelmHTTPCredentials certFile/keyFile are not supported for JFrog uploads"},
	}
	for _, tt := range credTests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			srv, requests := artifactoryServer(t)
			_, err := transformer(tt.creds).Transform(t.Context(), step(srv.URL, true, source()))
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				r.Empty(requests(), "no request may be sent")
				return
			}
			r.NoError(err)
			got := requests()
			r.Len(got, 2)
			for _, req := range got {
				if tt.wantBasic != nil {
					r.True(req.basic, "%s %s must use basic auth", req.method, req.path)
					r.Equal(tt.wantBasic, []string{req.username, req.password})
				} else {
					r.Equal(tt.wantAuth, req.authorization)
				}
			}
		})
	}

	t.Run("source digest mismatch", func(t *testing.T) {
		r := require.New(t)
		srv, _ := artifactoryServer(t)
		res := source()
		res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: "0000"}
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, true, res))
		r.ErrorContains(err, "digest mismatch: expected 0000, got "+chartDigest)
	})

	t.Run("local blob without repository provider", func(t *testing.T) {
		r := require.New(t)
		srv, requests := artifactoryServer(t)
		s := step(srv.URL, false, source())
		s.Spec.ComponentVersion = &JFrogHelmUploadComponentVersion{
			Repository: &runtime.Raw{Type: runtime.NewVersionedType("OCIRepository", "v1"), Data: []byte(`{"type":"OCIRepository/v1","baseUrl":"ghcr.io/source"}`)},
			Component:  "ocm.software/test",
			Version:    "1.0.0",
		}
		_, err := transformer(nil).Transform(t.Context(), s)
		r.ErrorContains(err, "no component version repository provider configured")
		r.Empty(requests())
	})

	t.Run("local blob is read from the source component version", func(t *testing.T) {
		r := require.New(t)
		srv, requests := artifactoryServer(t)
		repo := &localChartRepo{chart: chartTGZ}
		tr := transformer(credentialsByType{})
		tr.RepoProvider = &localChartRepoProvider{repo: repo}
		res := source()
		res.Access = &runtime.Raw{Type: runtime.NewVersionedType("LocalBlob", "v1"), Data: []byte(`{"type":"LocalBlob/v1","localReference":"sha256:abc","mediaType":"application/vnd.cncf.helm.chart.content.v1.tar+gzip"}`)}
		s := step(srv.URL, false, res)
		s.Spec.ComponentVersion = &JFrogHelmUploadComponentVersion{
			Repository: &runtime.Raw{Type: runtime.NewVersionedType("OCIRepository", "v1"), Data: []byte(`{"type":"OCIRepository/v1","baseUrl":"ghcr.io/source"}`)},
			Component:  "ocm.software/test",
			Version:    "1.0.0",
		}
		_, err := tr.Transform(t.Context(), s)
		r.NoError(err)
		r.Equal("ocm.software/test", repo.component)
		r.Equal("1.0.0", repo.version)
		r.Equal(runtime.Identity{"name": "renamed", "version": "9.9.9"}, repo.identity)
		got := requests()
		r.Len(got, 1)
		r.Equal("/artifactory/helm-local/mychart-0.1.0.tgz", got[0].path)
		r.Equal(chartTGZ, got[0].body)
	})
}
