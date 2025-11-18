package component

import (
	"bytes"
	"context"
	"crypto"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/fluxcd/pkg/runtime/patch"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ocmv1 "ocm.software/ocm/api/ocm/compdesc/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/kubernetes/controller/internal/util"
)

const (
	// LegacyNormalisationAlgo identifies the deprecated v3 JSON normalisation algorithm.
	LegacyNormalisationAlgo = "jsonNormalisation/v3"
)

// Matcher is a generic matcher function type
type Matcher[T any] func(T) bool

// Reconciler reconciles a Component object.
type Reconciler struct {
	*ocm.BaseReconciler

	Resolver  *resolution.Resolver
	OCMScheme *runtime.Scheme
}

var _ ocm.Reconciler = (*Reconciler)(nil)

var resourceIndex = ".spec.componentRef.Name"

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Create index for repository reference name from components to make sure to reconcile, when the base ocm-
	// repository changes.
	const fieldName = "spec.repositoryRef.name"
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Component{}, fieldName, func(obj client.Object) []string {
		component, ok := obj.(*v1alpha1.Component)
		if !ok {
			return nil
		}

		return []string{component.Spec.RepositoryRef.Name}
	}); err != nil {
		return fmt.Errorf("failed setting index fields: %w", err)
	}

	// This index is required to get all resources that reference a component. This is required to make sure that when
	// deleting the component, no resource exists anymore that references that component.
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Resource{}, resourceIndex, func(obj client.Object) []string {
		resource, ok := obj.(*v1alpha1.Resource)
		if !ok {
			return nil
		}

		return []string{resource.Spec.ComponentRef.Name}
	}); err != nil {
		return fmt.Errorf("failed setting index fields: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Component{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&v1alpha1.Repository{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				repository, ok := obj.(*v1alpha1.Repository)
				if !ok {
					return []reconcile.Request{}
				}

				// Get list of components that reference the repository
				list := &v1alpha1.ComponentList{}
				if err := r.List(ctx, list, client.MatchingFields{fieldName: repository.GetName()}); err != nil {
					return []reconcile.Request{}
				}

				// For every component that references the repository create a reconciliation request for that
				// component
				requests := make([]reconcile.Request, 0, len(list.Items))
				for _, component := range list.Items {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Namespace: component.GetNamespace(),
							Name:      component.GetName(),
						},
					})
				}

				return requests
			})).
		Watches(
			// Ensure to reconcile the component when an OCM resource changes that references this component.
			// We want to reconcile because the component-finalizer makes sure that the component is only deleted when
			// it is not referenced by any resource anymore. So, when the component is already marked for deletion, we
			// want to get notified about resource changes (e.g. deletion) to remove the component-finalizer
			// respectively.
			&v1alpha1.Resource{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				resource, ok := obj.(*v1alpha1.Resource)
				if !ok {
					return []reconcile.Request{}
				}

				component := &v1alpha1.Component{}
				if err := r.Get(ctx, client.ObjectKey{
					Namespace: resource.GetNamespace(),
					Name:      resource.Spec.ComponentRef.Name,
				}, component); err != nil {
					return []reconcile.Request{}
				}

				// Only reconcile if the component is marked for deletion
				if component.GetDeletionTimestamp().IsZero() {
					return []reconcile.Request{}
				}

				return []reconcile.Request{
					{NamespacedName: types.NamespacedName{
						Namespace: component.GetNamespace(),
						Name:      component.GetName(),
					}},
				}
			})).
		Complete(r)
}

// +kubebuilder:rbac:groups=delivery.ocm.software,resources=components,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=components/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=components/finalizers,verbs=update

// +kubebuilder:rbac:groups="",resources=secrets;configmaps;serviceaccounts,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=serviceaccounts/token,verbs=create
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

//nolint:funlen,cyclop // we do not want to cut the function at arbitrary points
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, err error) {
	logger := log.FromContext(ctx)
	logger.Info("starting reconciliation")

	component := &v1alpha1.Component{}
	if err := r.Get(ctx, req.NamespacedName, component); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	patchHelper := patch.NewSerialPatcher(component, r.Client)
	defer func(ctx context.Context) {
		err = errors.Join(err, status.UpdateStatus(ctx, patchHelper, component, r.EventRecorder, component.GetRequeueAfter(), err))
	}(ctx)

	if !component.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, component)
	}

	if updated := controllerutil.AddFinalizer(component, v1alpha1.ComponentFinalizer); updated {
		if err := r.Update(ctx, component); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}

		return ctrl.Result{Requeue: true}, nil
	}

	if component.Spec.Suspend {
		logger.Info("component is suspended, skipping reconciliation")

		return ctrl.Result{}, nil
	}

	logger.Info("prepare reconciling component")
	repo, err := util.GetReadyObject[v1alpha1.Repository, *v1alpha1.Repository](ctx, r.Client, client.ObjectKey{
		Namespace: component.GetNamespace(),
		Name:      component.Spec.RepositoryRef.Name,
	})
	if err != nil {
		// Note: Marking the component as not ready, when the repository is not ready is not completely valid. As the
		// component was potentially ready, then the repository changed, but that does not necessarily mean that the
		// component is not ready as well.
		// However, as the component is hard-dependant on the repository, we decided to mark it not ready as well.
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.GetResourceFailedReason, "OCM Repository is not ready")

		if errors.Is(err, util.NotReadyError{}) || errors.Is(err, util.DeletionError{}) {
			logger.Info(err.Error())

			// return no requeue as we watch the object for changes anyway
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to get ready repository: %w", err)
	}

	logger.Info("reconciling component")
	configs, err := ocm.GetEffectiveConfig(ctx, r.GetClient(), component)
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), component, v1alpha1.ConfigureContextFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get effective config: %w", err)
	}

	repoSpec, err := r.convertRepositorySpec(repo.Spec.RepositorySpec)
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), component, v1alpha1.ConfigureContextFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to convert repository spec: %w", err)
	}

	cacheBackedRepo, err := r.Resolver.NewCacheBackedRepository(ctx, &resolution.RepositoryOptions{
		RepositorySpec:    repoSpec,
		OCMConfigurations: configs,
		Namespace:         component.GetNamespace(),
	})
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), component, v1alpha1.ConfigureContextFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to create cache-backed repository: %w", err)
	}

	version, err := r.DetermineEffectiveVersionFromRepo(ctx, component, cacheBackedRepo)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.CheckVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to determine effective version: %w", err)
	}

	desc, err := cacheBackedRepo.GetComponentVersion(ctx, component.Spec.Component, version)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.GetComponentVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get component version: %w", err)
	}

	digestSpec, err := generateDigest(ctx, desc, LegacyNormalisationAlgo, crypto.SHA256.String())
	if err != nil {
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.CheckVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to generate digest: %w", err)
	}

	if len(component.Spec.Verify) > 0 {
		if err := verifyComponentVersion(ctx, desc, component.Spec.Verify); err != nil {
			status.MarkNotReady(r.EventRecorder, component, v1alpha1.GetComponentVersionFailedReason, err.Error())

			return ctrl.Result{}, fmt.Errorf("failed to verify component version: %w", err)
		}
	}

	logger.Info("updating status")
	component.Status.Component = v1alpha1.ComponentInfo{
		RepositorySpec: repo.Spec.RepositorySpec,
		Component:      component.Spec.Component,
		Version:        version,
		Digest: &ocmv1.DigestSpec{
			HashAlgorithm:          digestSpec.HashAlgorithm,
			NormalisationAlgorithm: digestSpec.NormalisationAlgorithm,
			Value:                  digestSpec.Value,
		},
	}

	component.Status.EffectiveOCMConfig = configs

	status.MarkReady(r.EventRecorder, component, "Applied version %s", version)

	return ctrl.Result{RequeueAfter: component.GetRequeueAfter()}, nil
}

func (r *Reconciler) reconcileDelete(ctx context.Context, component *v1alpha1.Component) error {
	// The component should only be deleted if no resource exists that references that component.
	resourceList := &v1alpha1.ResourceList{}
	if err := r.List(ctx, resourceList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(
			resourceIndex,
			client.ObjectKeyFromObject(component).Name,
		),
	}); err != nil {
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.DeletionFailedReason, err.Error())

		return fmt.Errorf("failed to list resource: %w", err)
	}

	if len(resourceList.Items) > 0 {
		var names []string
		for _, res := range resourceList.Items {
			names = append(names, fmt.Sprintf("%s/%s", res.Namespace, res.Name))
		}

		msg := fmt.Sprintf(
			"component cannot be removed as resources are still referencing it: %s",
			strings.Join(names, ","),
		)
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.DeletionFailedReason, msg)

		return errors.New(msg)
	}

	if updated := controllerutil.RemoveFinalizer(component, v1alpha1.ComponentFinalizer); updated {
		if err := r.Update(ctx, component); err != nil {
			status.MarkNotReady(r.EventRecorder, component, v1alpha1.DeletionFailedReason, err.Error())

			return fmt.Errorf("failed to remove finalizer: %w", err)
		}

		return nil
	}

	status.MarkNotReady(
		r.EventRecorder,
		component,
		v1alpha1.DeletionFailedReason,
		"component is being deleted and still has existing finalizers",
	)

	return nil
}

// convertRepositorySpec converts a JSON repository spec to a typed spec.
func (r *Reconciler) convertRepositorySpec(spec *apiextensionsv1.JSON) (runtime.Typed, error) {
	if spec == nil {
		return nil, fmt.Errorf("repository spec is nil")
	}

	raw := &runtime.Raw{}
	if err := r.OCMScheme.Decode(bytes.NewReader(spec.Raw), raw); err != nil {
		return nil, fmt.Errorf("failed to decode repository spec: %w", err)
	}

	// TODO: Handle CTF v1 to v2 conversion if needed
	if raw.GetType().Name == ctfv1.Type {
		return r.convertCTFOCMv1ToCTFOCMv2(raw)
	}

	obj, err := r.OCMScheme.NewObject(raw.Type)
	if err != nil {
		return nil, fmt.Errorf("failed to create new object: %w", err)
	}

	if err := r.OCMScheme.Convert(raw, obj); err != nil {
		return nil, fmt.Errorf("failed to convert repository spec: %w", err)
	}

	return obj, nil
}

func (r *Reconciler) convertCTFOCMv1ToCTFOCMv2(raw *runtime.Raw) (runtime.Typed, error) {
	values := make(map[string]interface{})
	if err := json.Unmarshal(raw.Data, &values); err != nil {
		return nil, fmt.Errorf("failed to decode repository spec: %w", err)
	}

	ctfType := &ctfv1.Repository{
		Type: runtime.Type{
			Version: "",
			Name:    "CommonTransportFormat",
		},
		Path:       values["filePath"].(string),
		AccessMode: ctfv1.AccessModeReadOnly,
	}

	return ctfType, nil
}

func (r *Reconciler) DetermineEffectiveVersionFromRepo(ctx context.Context, component *v1alpha1.Component,
	repo repository.ComponentVersionRepository,
) (string, error) {
	versions, err := repo.ListComponentVersions(ctx, component.Spec.Component)
	if err != nil {
		return "", fmt.Errorf("failed to list versions: %w", err)
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("component %s not found in repository", component.Spec.Component)
	}
	filter, err := ocm.RegexpFilter(component.Spec.SemverFilter)
	if err != nil {
		return "", reconcile.TerminalError(fmt.Errorf("failed to parse regexp filter: %w", err))
	}
	latestSemver, err := ocm.GetLatestValidVersion(ctx, versions, component.Spec.Semver, filter)
	if err != nil {
		return "", reconcile.TerminalError(fmt.Errorf("failed to get valid latest version: %w", err))
	}

	// we didn't yet reconcile anything, return whatever the retrieved version is.
	if component.Status.Component.Version == "" {
		return latestSemver.Original(), nil
	}

	currentSemver, err := semver.NewVersion(component.Status.Component.Version)
	if err != nil {
		return "", reconcile.TerminalError(fmt.Errorf("failed to check reconciled version: %w", err))
	}

	if latestSemver.GreaterThanEqual(currentSemver) {
		return latestSemver.Original(), nil
	}

	switch component.Spec.DowngradePolicy {
	case v1alpha1.DowngradePolicyDeny:
		return "", reconcile.TerminalError(fmt.Errorf("component version cannot be downgraded from version %s "+
			"to version %s", currentSemver.Original(), latestSemver.Original()))
	case v1alpha1.DowngradePolicyEnforce:
		return latestSemver.Original(), nil
	case v1alpha1.DowngradePolicyAllow:
		reconciledcv, err := repo.GetComponentVersion(ctx, component.Spec.Component, currentSemver.Original())
		if err != nil {
			return "", reconcile.TerminalError(fmt.Errorf("failed to get reconciled component version to check"+
				" downgradability: %w", err))
		}

		latestcv, err := repo.GetComponentVersion(ctx, component.Spec.Component, latestSemver.Original())
		if err != nil {
			return "", fmt.Errorf("failed to get component version: %w", err)
		}

		downgradable, err := ocm.IsDowngradable(ctx, reconciledcv, latestcv)
		if err != nil {
			return "", reconcile.TerminalError(fmt.Errorf("failed to check downgradability: %w", err))
		}
		if !downgradable {
			// keep requeueing, a greater component version could be published
			// semver constraint may even describe older versions and non-existing newer versions, so you have to check
			// for potential newer versions (current is downgradable to: > 1.0.3, latest is: < 1.1.0, but version 1.0.4
			// does not exist yet, but will be created)
			return "", fmt.Errorf("component version cannot be downgraded from version %s "+
				"to version %s", currentSemver.Original(), latestSemver.Original())
		}

		return latestSemver.Original(), nil
	default:
		return "", reconcile.TerminalError(errors.New("unknown downgrade policy: " + string(component.Spec.DowngradePolicy)))
	}
}

// verifyComponentVersion verifies the component version signatures.
// For now, we'll do basic digest verification. Full signature verification will be implemented
// once we have proper credential handling for the public keys from SecretRef/Value.
func verifyComponentVersion(ctx context.Context, desc *descruntime.Descriptor, verifications []v1alpha1.Verification) error {
	if err := isSafelyDigestible(&desc.Component); err != nil {
		return err
	}

	for _, verify := range verifications {
		var signature *descruntime.Signature
		for i := range desc.Signatures {
			if desc.Signatures[i].Name == verify.Signature {
				signature = &desc.Signatures[i]
				break
			}
		}

		if signature == nil {
			return fmt.Errorf("signature %q not found in component version", verify.Signature)
		}

		if err := verifyDigestMatchesDescriptor(ctx, desc, *signature); err != nil {
			return fmt.Errorf("digest verification failed for signature %q: %w", verify.Signature, err)
		}

		// TODO(Skarlso): Implement full signature verification using SecretRef/Value
	}

	return nil
}

// generateDigest computes a new digest for a descriptor with the given
// normalisation and hashing algorithms.
func generateDigest(
	ctx context.Context,
	desc *descruntime.Descriptor,
	normalisationAlgorithm string,
	hashAlgorithm string,
) (*descruntime.Digest, error) {
	normalisationAlgorithm = ensureNormalisationAlgo(ctx, normalisationAlgorithm)

	normalised, err := normalisation.Normalise(desc, normalisationAlgorithm)
	if err != nil {
		return nil, fmt.Errorf("normalising component version failed: %w", err)
	}

	hash, err := getSupportedHash(hashAlgorithm)
	if err != nil {
		return nil, err
	}

	h := hash.New()
	if _, err := h.Write(normalised); err != nil {
		return nil, fmt.Errorf("hashing component version failed: %w", err)
	}
	freshDigest := h.Sum(nil)

	return &descruntime.Digest{
		HashAlgorithm:          hash.String(),
		NormalisationAlgorithm: normalisationAlgorithm,
		Value:                  hex.EncodeToString(freshDigest),
	}, nil
}

// verifyDigestMatchesDescriptor ensures that a descriptor matches a digest
// provided by a signature.
func verifyDigestMatchesDescriptor(
	ctx context.Context,
	desc *descruntime.Descriptor,
	signature descruntime.Signature,
) error {
	signature.Digest.NormalisationAlgorithm = ensureNormalisationAlgo(ctx, signature.Digest.NormalisationAlgorithm)

	normalised, err := normalisation.Normalise(desc, signature.Digest.NormalisationAlgorithm)
	if err != nil {
		return fmt.Errorf("normalising component version failed: %w", err)
	}

	hash, err := getSupportedHash(signature.Digest.HashAlgorithm)
	if err != nil {
		return err
	}

	h := hash.New()
	if _, err := h.Write(normalised); err != nil {
		return fmt.Errorf("hashing component version failed: %w", err)
	}
	freshDigest := h.Sum(nil)

	digestFromSignature, err := hex.DecodeString(signature.Digest.Value)
	if err != nil {
		return fmt.Errorf("decoding digest from signature failed: %w", err)
	}

	if !bytes.Equal(freshDigest, digestFromSignature) {
		return fmt.Errorf("digest mismatch: descriptor %x vs signature %x", freshDigest, digestFromSignature)
	}
	return nil
}

// isSafelyDigestible validates that a component's references and resources
// contain consistent digests according rules.
func isSafelyDigestible(cd *descruntime.Component) error {
	for _, reference := range cd.References {
		if reference.Digest.HashAlgorithm == "" ||
			reference.Digest.NormalisationAlgorithm == "" ||
			reference.Digest.Value == "" {
			return fmt.Errorf("missing digest in componentReference for %s:%s", reference.Name, reference.Version)
		}
	}

	const AccessTypeNone = "None"
	for _, res := range cd.Resources {
		hasUsableAccess := res.Access != nil && res.Access.GetType().String() != AccessTypeNone
		if hasUsableAccess {
			if res.Digest == nil ||
				res.Digest.HashAlgorithm == "" ||
				res.Digest.NormalisationAlgorithm == "" ||
				res.Digest.Value == "" {
				return fmt.Errorf("missing digest in resource for %s:%s", res.Name, res.Version)
			}
		} else if res.Digest != nil {
			return fmt.Errorf("digest for resource with empty access not allowed %s:%s", res.Name, res.Version)
		}
	}
	return nil
}

// ensureNormalisationAlgo resolves the effective normalisation algorithm.
func ensureNormalisationAlgo(ctx context.Context, algo string) string {
	logger := log.FromContext(ctx).WithName("ensureNormalisationAlgo")
	if algo == LegacyNormalisationAlgo {
		logger.V(1).Info("legacy normalisation algorithm detected, using v4alpha1",
			"legacy", LegacyNormalisationAlgo,
			"new", v4alpha1.Algorithm,
		)
		return v4alpha1.Algorithm
	}
	return algo
}

// supportedHashes lists supported hashing algorithms keyed by their identifier
var supportedHashes = map[string]crypto.Hash{
	crypto.SHA256.String(): crypto.SHA256,
	crypto.SHA512.String(): crypto.SHA512,
}

// getSupportedHash looks up a crypto.Hash from its string identifier.
func getSupportedHash(name string) (crypto.Hash, error) {
	h, ok := supportedHashes[name]
	if !ok {
		return 0, fmt.Errorf("unsupported hash algorithm %q", name)
	}
	return h, nil
}
