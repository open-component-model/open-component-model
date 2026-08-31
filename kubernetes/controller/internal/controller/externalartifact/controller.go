// Package externalartifact produces a Flux ExternalArtifact
// (source.toolkit.fluxcd.io/v1) for every OCM Resource when the
// --enable-flux-external-artifacts-api feature is enabled. For each Ready
// Resource it downloads the referenced content (a local blob or an OCI artifact
// carrying a Helm chart or Kustomize overlay), packages it into a tar.gz served
// in-cluster, and records it in .status.artifact so Flux's kustomize- and
// helm-controllers can consume it via a sourceRef/chartRef of kind
// ExternalArtifact.
package externalartifact

import (
	"context"
	"errors"
	"fmt"
	"os"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/event"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
)

// ExternalArtifactFinalizer guards a Resource so its on-disk artifact is removed
// before the Resource (and its owned ExternalArtifact) is deleted.
const ExternalArtifactFinalizer = "finalizers.ocm.software/external-artifact"

// sourceDigestAnnotation records the OCM resource digest the current artifact
// was packaged from, letting a reconcile skip repackaging when it is unchanged.
const sourceDigestAnnotation = "delivery.ocm.software/source-digest"

// Reconciler reconciles a Resource into a Flux ExternalArtifact.
type Reconciler struct {
	*ocm.BaseReconciler

	// Resolver provides repository resolution and caching, shared with the other OCM controllers.
	Resolver *resolution.Resolver
	// PluginManager loads the plugins used to download resource content.
	PluginManager *manager.PluginManager
	// Storage packages and serves the artifacts.
	Storage *Storage
	// MaxResourceSizeBytes caps extracted resource content; zero disables it.
	MaxResourceSizeBytes int64
}

var _ ocm.Reconciler = (*Reconciler)(nil)

// SetupWithManager reconciles Resources (on generation or Ready/digest changes)
// and reacts to changes on the ExternalArtifacts it owns.
func (r *Reconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager, concurrency int) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Resource{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			ResourceReadyChangedPredicate{},
		))).
		Owns(&sourcev1.ExternalArtifact{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: concurrency}).
		Complete(r)
}

// +kubebuilder:rbac:groups=delivery.ocm.software,resources=resources,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=externalartifacts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=externalartifacts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list

//nolint:cyclop,funlen // the reconcile flow is linear and easier to read in one place
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	resource := &v1alpha1.Resource{}
	if err := r.Get(ctx, req.NamespacedName, resource); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion: drop the on-disk artifact, then release the finalizer.
	if !resource.GetDeletionTimestamp().IsZero() {
		return r.reconcileDelete(ctx, resource)
	}

	if resource.Spec.Suspend {
		return ctrl.Result{}, nil
	}

	// Only act on Resources that have successfully resolved their OCM content.
	if !apimeta.IsStatusConditionTrue(resource.Status.Conditions, v1alpha1.ReadyCondition) ||
		resource.Status.Resource == nil || resource.Status.Component == nil {
		logger.V(1).Info("resource is not ready yet, skipping external artifact generation")

		return ctrl.Result{}, nil
	}

	if updated := controllerutil.AddFinalizer(resource, ExternalArtifactFinalizer); updated {
		if err := r.Update(ctx, resource); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}

		return ctrl.Result{Requeue: true}, nil
	}

	// Ensure the ExternalArtifact object exists and is owned by the Resource.
	ea, err := r.ensureExternalArtifact(ctx, resource)
	if err != nil {
		return ctrl.Result{}, err
	}

	old := ea.DeepCopy()

	// Fast path / self-heal: skip repackaging when the recorded artifact is
	// still present on disk and the source digest is unchanged; repackage when
	// the store was wiped (e.g. pod restart on ephemeral storage) or the source
	// changed.
	if fresh, err := r.artifactIsFresh(ea, resource); err != nil {
		logger.V(1).Info("freshness check failed, will repackage", "error", err)
	} else if fresh {
		return ctrl.Result{}, nil
	}

	artifact, revision, err := r.buildArtifact(ctx, resource)
	if err != nil {
		setReadyCondition(ea, metav1.ConditionFalse, reasonFetchFailed, err.Error())
		event.New(r.EventRecorder, ea, nil, v1alpha1.EventSeverityError,
			"failed to produce artifact: %s", err.Error())
		if patchErr := r.patchStatus(ctx, ea, old); patchErr != nil {
			return ctrl.Result{}, errors.Join(err, patchErr)
		}

		return ctrl.Result{}, err
	}

	if err := r.recordSourceDigest(ctx, ea, old, resource); err != nil {
		return ctrl.Result{}, err
	}

	ea.Status.Artifact = artifact
	setReadyCondition(ea, metav1.ConditionTrue, reasonSucceeded,
		fmt.Sprintf("stored artifact for revision %s", revision))
	event.New(r.EventRecorder, ea, nil, v1alpha1.EventSeverityInfo,
		"stored artifact for revision %s", revision)

	if err := r.patchStatus(ctx, ea, old); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("reconciled external artifact", "revision", revision, "url", artifact.URL)

	return ctrl.Result{}, nil
}

// recordSourceDigest persists the source digest annotation so the next
// reconcile can take the fast path. It is a no-op when unchanged or absent.
func (r *Reconciler) recordSourceDigest(ctx context.Context, ea, old *sourcev1.ExternalArtifact, resource *v1alpha1.Resource) error {
	digest := resourceDigest(resource)
	if digest == "" || (ea.Annotations != nil && ea.Annotations[sourceDigestAnnotation] == digest) {
		return nil
	}
	if ea.Annotations == nil {
		ea.Annotations = map[string]string{}
	}
	ea.Annotations[sourceDigestAnnotation] = digest

	return r.patchAnnotations(ctx, ea, old)
}

// artifactIsFresh reports whether the recorded artifact is still valid: present
// on disk and packaged from the current source digest. A cheap size stat gates
// the common case; a full re-hash is the fallback when it is inconclusive.
func (r *Reconciler) artifactIsFresh(ea *sourcev1.ExternalArtifact, resource *v1alpha1.Resource) (bool, error) {
	if ea.Status.Artifact == nil {
		return false, nil
	}
	if want := resourceDigest(resource); want != "" && ea.Annotations[sourceDigestAnnotation] != want {
		return false, nil
	}
	if ea.Status.Artifact.Size != nil && r.Storage.SizeMatches(ea.Status.Artifact.Path, *ea.Status.Artifact.Size) {
		return true, nil
	}
	return r.Storage.Verify(ea.Status.Artifact.Path, ea.Status.Artifact.Digest)
}

// buildArtifact downloads the resource content, packages it into a tar.gz and
// returns the Flux Artifact together with a human-readable revision identifier.
func (r *Reconciler) buildArtifact(
	ctx context.Context,
	resource *v1alpha1.Resource,
) (*fluxArtifact, string, error) {
	tmpDir, err := os.MkdirTemp("", "ocm-external-artifact-*")
	if err != nil {
		return nil, "", fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	matched, err := r.resolveAndDownload(ctx, resource, tmpDir)
	if err != nil {
		return nil, "", err
	}

	result, err := r.Storage.Archive(ctx, v1alpha1.KindResource, resource.GetNamespace(), resource.GetName(), tmpDir)
	if err != nil {
		return nil, "", fmt.Errorf("failed to package artifact: %w", err)
	}

	revisionID := revisionIdentifier(resource, matched)
	artifact := r.Storage.Artifact(result, revisionID)

	// Best-effort cleanup of superseded revisions.
	if err := r.Storage.RemoveAllButCurrent(
		v1alpha1.KindResource, resource.GetNamespace(), resource.GetName(), result.Path,
	); err != nil {
		log.FromContext(ctx).Error(err, "failed to prune stale artifacts")
	}

	return artifact, artifact.Revision, nil
}

// ensureExternalArtifact creates the ExternalArtifact for a Resource if it does
// not yet exist and makes sure the Resource owns it.
func (r *Reconciler) ensureExternalArtifact(
	ctx context.Context,
	resource *v1alpha1.Resource,
) (*sourcev1.ExternalArtifact, error) {
	ea := &sourcev1.ExternalArtifact{}
	err := r.Get(ctx, client.ObjectKeyFromObject(resource), ea)
	switch {
	case apierrors.IsNotFound(err):
		ea = &sourcev1.ExternalArtifact{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resource.GetName(),
				Namespace: resource.GetNamespace(),
			},
			Spec: sourcev1.ExternalArtifactSpec{
				SourceRef: &fluxNamespacedObjectKindReference{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       v1alpha1.KindResource,
					Name:       resource.GetName(),
					Namespace:  resource.GetNamespace(),
				},
			},
		}
		if err := controllerutil.SetControllerReference(resource, ea, r.Scheme); err != nil {
			return nil, fmt.Errorf("failed to set owner reference: %w", err)
		}
		if err := r.Create(ctx, ea); err != nil {
			return nil, fmt.Errorf("failed to create external artifact: %w", err)
		}

		return ea, nil
	case err != nil:
		return nil, fmt.Errorf("failed to get external artifact: %w", err)
	}

	return ea, nil
}

// reconcileDelete removes the on-disk artifacts of a Resource and releases the
// finalizer. The ExternalArtifact itself is garbage-collected by Kubernetes via
// the owner reference.
func (r *Reconciler) reconcileDelete(ctx context.Context, resource *v1alpha1.Resource) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(resource, ExternalArtifactFinalizer) {
		return ctrl.Result{}, nil
	}

	if err := r.Storage.RemoveAll(v1alpha1.KindResource, resource.GetNamespace(), resource.GetName()); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove artifacts: %w", err)
	}

	controllerutil.RemoveFinalizer(resource, ExternalArtifactFinalizer)
	if err := r.Update(ctx, resource); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

// patchAnnotations persists metadata (annotation) changes on the
// ExternalArtifact when they differ from old.
func (r *Reconciler) patchAnnotations(ctx context.Context, ea, old *sourcev1.ExternalArtifact) error {
	if equality.Semantic.DeepEqual(ea.ObjectMeta, old.ObjectMeta) {
		return nil
	}
	if err := r.Patch(ctx, ea, client.MergeFrom(old)); err != nil {
		return fmt.Errorf("failed to patch external artifact metadata: %w", err)
	}

	return nil
}

// patchStatus persists status changes on the ExternalArtifact when they differ.
func (r *Reconciler) patchStatus(ctx context.Context, ea, old *sourcev1.ExternalArtifact) error {
	if equality.Semantic.DeepEqual(ea.Status, old.Status) {
		return nil
	}
	if err := r.Status().Patch(ctx, ea, client.MergeFrom(old)); err != nil {
		return fmt.Errorf("failed to patch external artifact status: %w", err)
	}

	return nil
}
