package externalartifact

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// artifactPortName is the container/Service port name the artifact server uses.
const artifactPortName = "artifacts"

// inClusterNamespacePath is where the ServiceAccount namespace is mounted.
const inClusterNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// clusterDNSSuffix is the in-cluster Service DNS suffix (trailing dot avoids
// search-domain expansion).
const clusterDNSSuffix = "svc.cluster.local."

// AdvertiseDiscovery holds the inputs for discovering the advertise address.
type AdvertiseDiscovery struct {
	// Namespace of this pod; defaults to the in-cluster ServiceAccount namespace.
	Namespace string
	// PodName of this pod; defaults to $POD_NAME then the hostname.
	PodName string
	// ContainerPort the artifact server listens on inside the pod.
	ContainerPort int32
}

// DiscoverAdvertiseAddress finds the Service that fronts this pod on the
// artifact port and returns the in-cluster address
// "<service>.<namespace>.svc.cluster.local.:<servicePort>" used to build
// ExternalArtifact URLs. It removes the need to hand-configure an advertise
// address that must exactly match the Service FQDN.
//
// It reads this pod's labels, then picks the namespace Service whose selector
// matches them and whose targetPort resolves to the artifact container port
// (disambiguating from the metrics/webhook Services).
//
// RBAC for the pod/service reads it performs is declared alongside the
// reconciler's markers in controller.go.
func DiscoverAdvertiseAddress(ctx context.Context, c client.Client, d AdvertiseDiscovery) (string, error) {
	ns := d.Namespace
	if ns == "" {
		ns = inClusterNamespace()
	}
	if ns == "" {
		return "", fmt.Errorf("could not determine namespace; set POD_NAMESPACE or run in-cluster")
	}

	podName := d.PodName
	if podName == "" {
		podName = os.Getenv("POD_NAME")
	}
	if podName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return "", fmt.Errorf("could not determine pod name: %w", err)
		}
		podName = hostname
	}

	pod := &corev1.Pod{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: podName}, pod); err != nil {
		return "", fmt.Errorf("failed to get pod %s/%s: %w", ns, podName, err)
	}

	services := &corev1.ServiceList{}
	if err := c.List(ctx, services, client.InNamespace(ns)); err != nil {
		return "", fmt.Errorf("failed to list services in %q: %w", ns, err)
	}

	for i := range services.Items {
		svc := &services.Items[i]
		if len(svc.Spec.Selector) == 0 {
			continue
		}
		if !labels.SelectorFromSet(svc.Spec.Selector).Matches(labels.Set(pod.Labels)) {
			continue
		}
		if port, ok := servicePortForTarget(svc, d.ContainerPort); ok {
			return fmt.Sprintf("%s.%s.%s:%d", svc.Name, ns, clusterDNSSuffix, port), nil
		}
	}

	return "", fmt.Errorf(
		"no Service in namespace %q selects this pod and exposes the artifact port %d; "+
			"expose the manager on the artifact port (name %q) or set the advertise address explicitly",
		ns, d.ContainerPort, artifactPortName)
}

// servicePortForTarget returns the Service port that forwards to the artifact
// container port, matching by named targetPort ("artifacts"), numeric
// targetPort, or (when targetPort is unset) the port number itself.
func servicePortForTarget(svc *corev1.Service, containerPort int32) (int32, bool) {
	for _, p := range svc.Spec.Ports {
		switch {
		case p.TargetPort.Type == intstr.String && p.TargetPort.StrVal == artifactPortName:
			return p.Port, true
		case p.TargetPort.Type == intstr.Int && p.TargetPort.IntVal == containerPort:
			return p.Port, true
		case p.TargetPort.IntVal == 0 && p.TargetPort.StrVal == "" && p.Port == containerPort:
			return p.Port, true
		}
	}

	return 0, false
}

// inClusterNamespace reads the pod namespace from the ServiceAccount mount,
// falling back to $POD_NAMESPACE. Returns "" when neither is available.
func inClusterNamespace() string {
	if data, err := os.ReadFile(inClusterNamespacePath); err == nil {
		if ns := string(data); ns != "" {
			return ns
		}
	}

	return os.Getenv("POD_NAMESPACE")
}

// PortFromAddress extracts the numeric port from a bind address like ":9091" or
// "0.0.0.0:9091".
func PortFromAddress(addr string) (int32, error) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("invalid address %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("invalid port in address %q: %w", addr, err)
	}

	return int32(port), nil
}
