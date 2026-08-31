package externalartifact

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPortFromAddress(t *testing.T) {
	cases := []struct {
		addr    string
		want    int32
		wantErr bool
	}{
		{":9091", 9091, false},
		{"0.0.0.0:8080", 8080, false},
		{"nonsense", 0, true},
		{":notaport", 0, true},
	}
	for _, tc := range cases {
		got, err := PortFromAddress(tc.addr)
		if tc.wantErr {
			if err == nil {
				t.Errorf("PortFromAddress(%q): expected error", tc.addr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("PortFromAddress(%q) = (%d, %v), want (%d, nil)", tc.addr, got, err, tc.want)
		}
	}
}

func TestServicePortForTarget(t *testing.T) {
	const cp int32 = 9091
	cases := []struct {
		name string
		ports []corev1.ServicePort
		want  int32
		ok    bool
	}{
		{"named targetPort", []corev1.ServicePort{{Port: 9091, TargetPort: intstr.FromString("artifacts")}}, 9091, true},
		{"numeric targetPort", []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromInt32(cp)}}, 80, true},
		{"unset targetPort matches port", []corev1.ServicePort{{Port: cp}}, cp, true},
		{"no match", []corev1.ServicePort{{Port: 443, TargetPort: intstr.FromInt32(8443)}}, 0, false},
	}
	for _, tc := range cases {
		svc := &corev1.Service{Spec: corev1.ServiceSpec{Ports: tc.ports}}
		got, ok := servicePortForTarget(svc, cp)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: servicePortForTarget = (%d, %v), want (%d, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDiscoverAdvertiseAddress(t *testing.T) {
	const ns, podName = "ocm-system", "manager-abc"
	podLabels := map[string]string{"app": "ocm", "control-plane": "controller-manager"}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: ns, Labels: podLabels},
	}
	// The Service that fronts the pod on the artifact port (named targetPort).
	artifactSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "ocm-controller-manager", Namespace: ns},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"control-plane": "controller-manager"},
			Ports:    []corev1.ServicePort{{Name: "artifacts", Port: 9091, TargetPort: intstr.FromString("artifacts")}},
		},
	}
	// A metrics Service that also selects the pod but not the artifact port.
	metricsSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "ocm-metrics", Namespace: ns},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"control-plane": "controller-manager"},
			Ports:    []corev1.ServicePort{{Name: "https", Port: 8443, TargetPort: intstr.FromInt32(8443)}},
		},
	}

	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(pod, metricsSvc, artifactSvc).Build()

	got, err := DiscoverAdvertiseAddress(context.Background(), c, AdvertiseDiscovery{
		Namespace: ns, PodName: podName, ContainerPort: 9091,
	})
	if err != nil {
		t.Fatalf("DiscoverAdvertiseAddress: %v", err)
	}
	want := "ocm-controller-manager.ocm-system.svc.cluster.local.:9091"
	if got != want {
		t.Errorf("advertise address = %q, want %q", got, want)
	}
}

func TestDiscoverAdvertiseAddressNoMatch(t *testing.T) {
	const ns, podName = "ocm-system", "manager-abc"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: ns, Labels: map[string]string{"app": "ocm"}},
	}
	// Service exists but exposes a different port only.
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "ocm-metrics", Namespace: ns},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "ocm"},
			Ports:    []corev1.ServicePort{{Name: "https", Port: 8443, TargetPort: intstr.FromInt32(8443)}},
		},
	}
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(pod, svc).Build()

	if _, err := DiscoverAdvertiseAddress(context.Background(), c, AdvertiseDiscovery{
		Namespace: ns, PodName: podName, ContainerPort: 9091,
	}); err == nil {
		t.Error("expected error when no Service exposes the artifact port")
	}
}
