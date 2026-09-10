package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	internaldiscovery "ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/discovery"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/util"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Reconciler reconciles Discovery objects: it resolves the complete
// transitive component graph of the referenced ready Component, evaluates the
// compiled selector/extraction query against it, and atomically publishes the
// resulting payload and conditions into the Discovery status.
//
// The reconciler is synchronous: contrary to BaseReconciler.PluginManagerFor,
// the request-scoped plugin manager is built with the reconcile context so
// cancellation propagates into plugin calls. It does not use the resolution
// worker pool; repository access happens through a request-scoped resolver
// rooted at the referenced Component's repository spec.
type Reconciler struct {
	*ocm.BaseReconciler

	// SafetyInterval is the controller-wide interval after which a
	// successfully reconciled Discovery is re-queued for a full discovery,
	// jittered by ±10%. It insures against missed watch events. A zero
	// interval disables safety scheduling.
	SafetyInterval time.Duration

	// randFloat yields values in [0.0, 1.0) for the safety-interval jitter.
	// It is injectable for tests; nil defaults to math/rand.Float64.
	randFloat func() float64
}

var _ ocm.Reconciler = (*Reconciler)(nil)

// +kubebuilder:rbac:groups=delivery.ocm.software,resources=discoveries,verbs=get;list;watch
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=discoveries/status,verbs=get;update;patch

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("starting reconciliation")

	discovery := &v1alpha1.Discovery{}
	if err := r.Get(ctx, req.NamespacedName, discovery); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Deleting and suspended objects exit without status advancement: the old
	// conditions and payload are retained as-is.
	if !discovery.GetDeletionTimestamp().IsZero() {
		logger.Info("discovery is being deleted, skipping reconciliation")
		return ctrl.Result{}, nil
	}
	if discovery.Spec.Suspend {
		logger.Info("discovery is suspended, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	old := discovery.DeepCopy()
	result, rerr := r.reconcile(ctx, discovery)
	if perr := r.publish(ctx, old, discovery); perr != nil {
		if rerr == nil && isPayloadTooLarge(perr) {
			// The freshly computed payload is rejected by the API server. The
			// fallback re-reads the object and writes only failure conditions,
			// retaining the persisted payload, config, and observed generation.
			// A successful fallback is terminal (no requeue); a failing
			// fallback write is retryable.
			if ferr := r.publishPayloadTooLarge(ctx, discovery); ferr != nil {
				return ctrl.Result{}, ferr
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, perr
	}

	return result, rerr
}

// reconcile executes the discovery pipeline against discovery and mutates its
// status in memory. Status publication happens separately in Reconcile.
//
// Error classification:
//   - selector/extraction compilation and evaluation failures are terminal
//     (Stalled + reconcile.TerminalError); they require a change to recover.
//   - dependency, configuration, repository, and traversal failures are
//     retryable and follow the normal controller-runtime backoff semantics.
//
//nolint:funlen,cyclop // the pipeline is intentionally linear; splitting it would obscure the failure-semantics
func (r *Reconciler) reconcile(ctx context.Context, discovery *v1alpha1.Discovery) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	component, err := util.GetReadyObject[v1alpha1.Component, *v1alpha1.Component](ctx, r.Client, client.ObjectKey{
		Namespace: discovery.GetNamespace(),
		Name:      discovery.Spec.ComponentRef.Name,
	})
	if err != nil {
		r.markFailure(discovery, v1alpha1.ResourceIsNotAvailable, err)

		var notReadyErr util.NotReadyError
		var deletionErr util.DeletionError
		if errors.As(err, &notReadyErr) || errors.As(err, &deletionErr) {
			logger.Info("component is not available", "error", err)
			// Retryable: the error-driven backoff is the self-healing requeue
			// in case a Component watch event is missed.
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, fmt.Errorf("failed to get ready component: %w", err)
	}

	info := component.Status.Component
	if info.Component == "" || info.Version == "" || info.RepositorySpec == nil {
		err := fmt.Errorf("component %s has no complete resolved identity and repository spec", component.GetName())
		r.markFailure(discovery, v1alpha1.ResourceIsNotAvailable, err)

		return ctrl.Result{}, err
	}

	configs, err := ocm.GetEffectiveConfig(ctx, r.GetClient(), discovery, component)
	if err != nil {
		r.markFailure(discovery, v1alpha1.GetConfigurationFailedReason, err)

		return ctrl.Result{}, fmt.Errorf("failed to get effective config: %w", err)
	}

	// Publish the effective config first so the used configuration references
	// stay observable even when a subsequent step fails. The returned error
	// persists the status and requeues; the next reconcile proceeds past this
	// point with an unchanged config.
	if !equality.Semantic.DeepEqual(discovery.Status.EffectiveOCMConfig, configs) {
		discovery.Status.EffectiveOCMConfig = configs
		return ctrl.Result{}, errors.New("effective ocm config changed")
	}

	query, err := internaldiscovery.Compile(ctx, &discovery.Spec)
	if err != nil {
		var selErr *internaldiscovery.SelectorError
		if errors.As(err, &selErr) {
			r.markStalled(discovery, v1alpha1.SelectorFailedReason, err)
			return ctrl.Result{}, reconcile.TerminalError(err)
		}
		var extErr *internaldiscovery.ExtractError
		if errors.As(err, &extErr) {
			r.markStalled(discovery, v1alpha1.ExtractFailedReason, err)
			return ctrl.Result{}, reconcile.TerminalError(err)
		}
		r.markFailure(discovery, v1alpha1.SelectorFailedReason, err)

		return ctrl.Result{}, err
	}

	cfg, err := configuration.LoadConfigurations(ctx, r.Client, discovery.GetNamespace(), configs)
	if err != nil {
		r.markFailure(discovery, v1alpha1.GetConfigurationFailedReason, err)

		return ctrl.Result{}, fmt.Errorf("failed to load configurations: %w", err)
	}

	if r.NewPluginManager == nil {
		err := errors.New("no plugin manager factory configured on the reconciler")
		r.markFailure(discovery, v1alpha1.GetConfigurationFailedReason, err)

		return ctrl.Result{}, err
	}
	var genericCfg *genericv1.Config
	if cfg != nil {
		genericCfg = cfg.Config
	}
	pm, err := r.NewPluginManager(ctx, genericCfg)
	if err != nil {
		r.markFailure(discovery, v1alpha1.GetConfigurationFailedReason, err)

		return ctrl.Result{}, fmt.Errorf("failed to create plugin manager: %w", err)
	}

	spec := &runtime.Raw{}
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Decode(bytes.NewReader(info.RepositorySpec.Raw), spec); err != nil {
		r.markFailure(discovery, v1alpha1.GetRepositoryFailedReason, err)

		return ctrl.Result{}, fmt.Errorf("failed to decode repository spec: %w", err)
	}

	var credentialGraph credentials.Resolver
	if cfg != nil {
		credentialGraph, err = setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
			PluginManager: pm,
			Logger:        &logger,
		})
		if err != nil {
			r.markFailure(discovery, v1alpha1.GetRepositoryFailedReason, err)

			return ctrl.Result{}, fmt.Errorf("failed to create credential graph: %w", err)
		}
	}

	resolver, err := resolvers.NewFromConfig(ctx, genericCfg, ocirepository.Scheme, resolvers.Options{
		RepoProvider:      pm.ComponentVersionRepositoryRegistry,
		CredentialGraph:   credentialGraph,
		ComponentPatterns: []string{info.Component},
	}, spec)
	if err != nil {
		r.markFailure(discovery, v1alpha1.GetRepositoryFailedReason, err)

		return ctrl.Result{}, fmt.Errorf("failed to create repository resolver: %w", err)
	}

	graph, err := internaldiscovery.Traverse(ctx,
		internaldiscovery.ComponentKey{Name: info.Component, Version: info.Version},
		resolver)
	if err != nil {
		r.markFailure(discovery, v1alpha1.ResolutionFailedReason, err)

		return ctrl.Result{}, fmt.Errorf("failed to resolve the transitive component graph: %w", err)
	}
	logger.V(1).Info("resolved transitive component graph", "components", len(graph.Descriptors))

	filtered, err := query.Filter(ctx, *graph)
	if err != nil {
		var selErr *internaldiscovery.SelectorError
		if errors.As(err, &selErr) {
			r.markStalled(discovery, v1alpha1.SelectorFailedReason, err)
			return ctrl.Result{}, reconcile.TerminalError(err)
		}
		r.markFailure(discovery, v1alpha1.SelectorFailedReason, err)

		return ctrl.Result{}, err
	}

	payload, err := query.Project(ctx, filtered)
	if err != nil {
		var extErr *internaldiscovery.ExtractError
		if errors.As(err, &extErr) {
			r.markStalled(discovery, v1alpha1.ExtractFailedReason, err)
			return ctrl.Result{}, reconcile.TerminalError(err)
		}
		r.markFailure(discovery, v1alpha1.ExtractFailedReason, err)

		return ctrl.Result{}, err
	}

	if err := r.setPayload(discovery, payload); err != nil {
		r.markFailure(discovery, v1alpha1.MarshalFailedReason, err)

		return ctrl.Result{}, err
	}

	r.markSuccess(discovery, payloadReason(payload), payloadMessage(payload))

	return ctrl.Result{RequeueAfter: r.safetyRequeueAfter()}, nil
}

// setPayload swaps the status payload in one in-memory update: the selected
// field is set (possibly to an empty list), the other field is cleared. The
// payload of a previous output mode is replaced atomically on success.
func (r *Reconciler) setPayload(discovery *v1alpha1.Discovery, payload *internaldiscovery.Payload) error {
	if payload.Extracted != nil {
		records := make([]v1alpha1.ExtractedRecord, 0, len(payload.Extracted))
		for _, record := range payload.Extracted {
			typed := make(v1alpha1.ExtractedRecord, len(record))
			for name, value := range record {
				raw, err := json.Marshal(value)
				if err != nil {
					return fmt.Errorf("failed to marshal extracted field %q: %w", name, err)
				}
				typed[name] = apiextensionsv1.JSON{Raw: raw}
			}
			records = append(records, typed)
		}
		discovery.Status.Extracted = records
		discovery.Status.Components = nil

		return nil
	}

	components := make([]apiextensionsv1.JSON, 0, len(payload.Components))
	for _, raw := range payload.Components {
		components = append(components, apiextensionsv1.JSON{Raw: raw})
	}
	discovery.Status.Components = components
	discovery.Status.Extracted = nil

	return nil
}

// payloadReason returns the Ready condition reason for a successful
// evaluation: Succeeded, NoReferencesMatched, or NoComponentsMatched.
func payloadReason(payload *internaldiscovery.Payload) string {
	switch payload.Reason {
	case internaldiscovery.EmptyReasonNoReferencesMatched:
		return v1alpha1.NoReferencesMatchedReason
	case internaldiscovery.EmptyReasonNoComponentsMatched:
		return v1alpha1.NoComponentsMatchedReason
	default:
		return v1alpha1.SucceededReason
	}
}

func payloadMessage(payload *internaldiscovery.Payload) string {
	switch payload.Reason {
	case internaldiscovery.EmptyReasonNoReferencesMatched:
		return "discovery succeeded, but no references matched the reference selector"
	case internaldiscovery.EmptyReasonNoComponentsMatched:
		return "discovery succeeded, but no components matched the component selector"
	default:
		return fmt.Sprintf("discovery succeeded with %d results", resultCount(payload))
	}
}

func resultCount(payload *internaldiscovery.Payload) int {
	if payload.Extracted != nil {
		return len(payload.Extracted)
	}
	return len(payload.Components)
}
