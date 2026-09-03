package ocm

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

// PluginManagerFunc used create a per-request plugin manager.
type PluginManagerFunc func(ctx context.Context, cfg *genericv1.Config) (*manager.PluginManager, error)

type Reconciler interface {
	GetClient() ctrl.Client
	GetScheme() *runtime.Scheme
	GetEventRecorder() record.EventRecorder
}

type BaseReconciler struct {
	ctrl.Client
	record.EventRecorder

	Scheme           *runtime.Scheme
	NewPluginManager PluginManagerFunc
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

// PluginManagerFor creates a new plugin manager by wrapping the original context into a without cancelled one.
// That's because we don't want the plugin's context to be dependent on the request context but the WorkerPool's
// context. The resolver will get the right context from the WorkerPool.
func (r *BaseReconciler) PluginManagerFor(ctx context.Context, cfg *configuration.Configuration) (*manager.PluginManager, error) {
	if r.NewPluginManager == nil {
		return nil, errors.New("no plugin manager factory configured on the reconciler")
	}

	var generic *genericv1.Config
	if cfg != nil {
		generic = cfg.Config
	}

	return r.NewPluginManager(context.WithoutCancel(ctx), generic)
}
