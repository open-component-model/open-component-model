package ocm

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

type Reconciler interface {
	GetClient() ctrl.Client
	GetScheme() *runtime.Scheme
	GetEventRecorder() record.EventRecorder
}

type BaseReconciler struct {
	ctrl.Client
	Scheme *runtime.Scheme
	record.EventRecorder
}

func (r *BaseReconciler) GetClient() ctrl.Client {
	return r.Client
}

func (r *BaseReconciler) GetScheme() *runtime.Scheme {
	return r.Scheme
}

func (r *BaseReconciler) GetEventRecorder() record.EventRecorder {
	return r.EventRecorder
}
