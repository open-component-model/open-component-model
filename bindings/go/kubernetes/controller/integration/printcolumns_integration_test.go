package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// tableAccept asks the API server to render an object with the CRD's printer
// columns, the same request kubectl get sends.
const tableAccept = "application/json;as=Table;g=meta.k8s.io;v=v1"

// Test_Integration_PrintColumns creates one object of every OCM kind, fills in
// the fields its printer columns read, and checks the cells the API server
// renders. A JSONPath that does not resolve on a real object gives an empty
// cell, which a check on the generated CRD YAML alone cannot see.
func Test_Integration_PrintColumns(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	testEnv := test.NewEnvTest()
	cfg, err := testEnv.Start()
	r.NoError(err)
	t.Cleanup(func() { assert.NoError(t, testEnv.Stop()) })

	scheme := k8sruntime.NewScheme()
	r.NoError(v1alpha1.AddToScheme(scheme))
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	r.NoError(err)
	httpClient, err := rest.HTTPClientFor(cfg)
	r.NoError(err)

	const namespace = "default"
	meta := func(name string, namespaced bool) metav1.ObjectMeta {
		if namespaced {
			return metav1.ObjectMeta{Name: name, Namespace: namespace}
		}
		return metav1.ObjectMeta{Name: name}
	}
	repositorySpec := &apiextensionsv1.JSON{Raw: []byte(`{"type":"OCIRegistry","baseUrl":"ghcr.io/acme"}`)}
	componentInfo := v1alpha1.ComponentInfo{RepositorySpec: repositorySpec, Component: "acme.org/hello", Version: "1.2.3"}

	tests := []struct {
		name string
		obj  v1alpha1.OCMK8SObject
		// status fills in the status fields the identifying columns read.
		status func(obj v1alpha1.OCMK8SObject)
		// notReady sets the Ready condition to False instead of True.
		notReady bool
		want     map[string]string
	}{
		{
			name: "component",
			obj: &v1alpha1.Component{
				ObjectMeta: meta("my-component", true),
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{Name: "my-repository"},
					Component:     "acme.org/hello",
					Semver:        ">=1.0.0",
					Interval:      metav1.Duration{Duration: time.Minute},
				},
			},
			status: func(obj v1alpha1.OCMK8SObject) {
				obj.(*v1alpha1.Component).Status.Component = componentInfo
			},
			notReady: true,
			want:     map[string]string{"Component": "acme.org/hello", "Version": "1.2.3"},
		},
		{
			name: "repository",
			obj: &v1alpha1.Repository{
				ObjectMeta: meta("my-repository", true),
				Spec: v1alpha1.RepositorySpec{
					RepositorySpec: repositorySpec,
					Interval:       metav1.Duration{Duration: time.Minute},
				},
			},
			want: map[string]string{"Type": "OCIRegistry"},
		},
		{
			name: "resource",
			obj: &v1alpha1.Resource{
				ObjectMeta: meta("my-resource", true),
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{Name: "my-component"},
					Resource: v1alpha1.ResourceID{ByReference: v1alpha1.ResourceReference{
						Resource: runtime.Identity{"name": "chart"},
					}},
				},
			},
			status: func(obj v1alpha1.OCMK8SObject) {
				res := obj.(*v1alpha1.Resource)
				res.Status.Resource = &v1alpha1.ResourceInfo{
					Name:    "chart",
					Type:    "helmChart",
					Version: "4.5.6",
					Access:  apiextensionsv1.JSON{Raw: []byte(`{"type":"localBlob"}`)},
				}
				res.Status.Component = &componentInfo
			},
			want: map[string]string{"Component Ref": "my-component", "Resource": "chart", "Version": "4.5.6"},
		},
		{
			name: "deployer",
			obj: &v1alpha1.Deployer{
				ObjectMeta: meta("my-deployer", false),
				Spec: v1alpha1.DeployerSpec{
					ResourceRef: v1alpha1.ObjectKey{Namespace: namespace, Name: "my-resource"},
				},
			},
			want: map[string]string{"Resource Namespace": namespace, "Resource Ref": "my-resource"},
		},
		{
			name: "namespaced deployer",
			obj: &v1alpha1.NamespacedDeployer{
				ObjectMeta: meta("my-namespaced-deployer", true),
				Spec: v1alpha1.NamespacedDeployerSpec{
					ResourceRef: corev1.LocalObjectReference{Name: "my-resource"},
				},
			},
			want: map[string]string{"Resource Ref": "my-resource"},
		},
		{
			name: "replication",
			obj: &v1alpha1.Replication{
				ObjectMeta: meta("my-replication", true),
				Spec: v1alpha1.ReplicationSpec{
					ComponentRef:        corev1.LocalObjectReference{Name: "my-component"},
					TargetRepositoryRef: corev1.LocalObjectReference{Name: "target-repository"},
				},
			},
			status: func(obj v1alpha1.OCMK8SObject) {
				obj.(*v1alpha1.Replication).Status.LastTransferredVersion = "1.2.3"
			},
			want: map[string]string{"Component Ref": "my-component", "Target": "target-repository", "Version": "1.2.3"},
		},
		{
			name: "discovery",
			obj: &v1alpha1.Discovery{
				ObjectMeta: meta("my-discovery", true),
				Spec: v1alpha1.DiscoverySpec{
					ComponentRef: corev1.LocalObjectReference{Name: "my-component"},
				},
			},
			want: map[string]string{"Component Ref": "my-component"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			r.NoError(c.Create(ctx, tt.obj))
			if tt.status != nil {
				tt.status(tt.obj)
			}
			readyStatus, reason, message := metav1.ConditionTrue, v1alpha1.SucceededReason, tt.name+" is ready"
			reconcilingStatus := metav1.ConditionFalse
			if tt.notReady {
				readyStatus, reason, message = metav1.ConditionFalse, v1alpha1.GetRepositoryFailedReason, tt.name+" is not ready"
				reconcilingStatus = metav1.ConditionTrue
			}
			// Reconciling comes first and differs from Ready in status and
			// message, so a column that does not filter on the Ready type
			// shows a wrong cell.
			tt.obj.SetConditions([]metav1.Condition{
				{
					Type:               v1alpha1.ReconcilingCondition,
					Status:             reconcilingStatus,
					Reason:             v1alpha1.ProgressingWithRetryReason,
					Message:            tt.name + " is reconciling",
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               v1alpha1.ReadyCondition,
					Status:             readyStatus,
					Reason:             reason,
					Message:            message,
					LastTransitionTime: metav1.Now(),
				},
			})
			r.NoError(c.Status().Update(ctx, tt.obj))

			cells := tableCells(t, c, httpClient, cfg.Host, tt.obj)

			r.NotEmpty(cells["Age"], "Age column is empty")
			delete(cells, "Age")

			want := map[string]string{"Name": tt.obj.GetName(), "Ready": string(readyStatus), "Status": message}
			for column, value := range tt.want {
				want[column] = value
			}
			r.Equal(want, cells)
		})
	}
}

// tableCells requests obj in the Table format and returns its single row,
// keyed by column name.
func tableCells(t *testing.T, c client.Client, httpClient *http.Client, host string, obj client.Object) map[string]string {
	t.Helper()
	r := require.New(t)

	gvk, err := apiutil.GVKForObject(obj, c.Scheme())
	r.NoError(err)
	mapping, err := c.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	r.NoError(err)

	segments := []string{"apis", gvk.Group, gvk.Version}
	if mapping.Scope.Name() == apimeta.RESTScopeNameNamespace {
		segments = append(segments, "namespaces", obj.GetNamespace())
	}
	segments = append(segments, mapping.Resource.Resource, obj.GetName())
	target, err := url.JoinPath(host, segments...)
	r.NoError(err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	r.NoError(err)
	req.Header.Set("Accept", tableAccept)
	resp, err := httpClient.Do(req)
	r.NoError(err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	r.NoError(err)
	r.Equal(http.StatusOK, resp.StatusCode, string(body))

	var table metav1.Table
	r.NoError(json.Unmarshal(body, &table))
	r.Len(table.Rows, 1)
	row := table.Rows[0].Cells
	r.Len(row, len(table.ColumnDefinitions))

	cells := make(map[string]string, len(row))
	for i, column := range table.ColumnDefinitions {
		// A JSONPath that matches nothing renders as null.
		if row[i] == nil {
			cells[column.Name] = ""
			continue
		}
		value, ok := row[i].(string)
		r.True(ok, "column %s has non-string cell %v", column.Name, row[i])
		cells[column.Name] = value
	}
	return cells
}
