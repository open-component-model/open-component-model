package ocm

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"sync"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/utils/lru"
	"ocm.software/ocm/api/datacontext"
	"ocm.software/ocm/api/ocm"
	"ocm.software/ocm/api/ocm/extensions/attrs/signingattr"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

var (
	// Prometheus gauges tracking cache sizes for sessions and contexts.
	sessionCacheSizeMetric *prometheus.GaugeVec
	contextCacheSizeMetric *prometheus.GaugeVec
)

// MustRegisterMetrics registers metrics and panics on error.
// Intended to be called during process startup.
func MustRegisterMetrics(registerer prometheus.Registerer) {
	if err := RegisterMetrics(registerer); err != nil {
		panic(err)
	}
}

// RegisterMetrics registers Prometheus metrics for session/context cache sizes.
// Uses errors.Join to return multiple registration errors if they occur.
func RegisterMetrics(registerer prometheus.Registerer) error {
	return errors.Join(
		registerer.Register(sessionCacheSizeMetric),
		registerer.Register(contextCacheSizeMetric),
	)
}

// init initializes Prometheus metric definitions.
func init() {
	sessionCacheSizeMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "session_cache_size",
		Help: "number of objects in cache",
	}, []string{"name"})
	contextCacheSizeMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "context_cache_size",
		Help: "number of objects in cache",
	}, []string{"name"})
}

// Compile-time assertion that ContextCache implements manager.Runnable.
var _ manager.Runnable = (*ContextCache)(nil)

// NewContextCache constructs a ContextCache with given name, size, and k8s client.
func NewContextCache(name string, contextCacheSize, sessionCacheSize int, client ctrl.Client, logger logr.Logger) *ContextCache {
	c := &ContextCache{
		lookupClient: client,
		logger:       logger,
	}

	// Cache for contexts, with eviction finalizer.
	c.contexts = lru.NewWithEvictionFunc(contextCacheSize, func(key lru.Key, value any) {
		defer contextCacheSizeMetric.WithLabelValues(name).Dec()
		ctx := value.(ocm.Context) //nolint:forcetypeassert // safe cast
		if err := ctx.Finalize(); err != nil {
			c.logger.Error(err, "failed to finalize context", "key", key)
		}
	})

	// Cache for sessions, with eviction finalizer.
	c.sessions = lru.NewWithEvictionFunc(sessionCacheSize, func(key lru.Key, value any) {
		defer contextCacheSizeMetric.WithLabelValues(name).Dec()
		session := value.(ocm.Session) //nolint:forcetypeassert // safe cast
		if err := session.Close(); err != nil {
			c.logger.Error(err, "failed to close session", "key", key)
		}
	})

	return c
}

// ContextCache holds LRU caches for OCM contexts and sessions.
// Contexts are expensive to build/configure, sessions are bound to repositories.
// Eviction handlers clean up resources when entries are removed.
type ContextCache struct {
	ctx  context.Context //nolint:containedctx // context is passed via Start from manager
	name string

	contexts *lru.Cache // cache of ocm.Context objects
	sessions *lru.Cache // cache of ocm.Session objects

	lookupClient ctrl.Reader // k8s client used for fetching configuration

	logger logr.Logger

	retrievalLock sync.Mutex
}

// Start initializes caches with eviction functions and blocks until context is done.
// When the provided context is canceled, all cached contexts and sessions are cleared.
func (m *ContextCache) Start(ctx context.Context) error {
	if m.ctx != nil {
		return fmt.Errorf("already started")
	}
	m.ctx = ctx
	<-ctx.Done()
	m.Clear()

	return nil
}

// GetSessionOptions encapsulates all parameters required to obtain or create an OCM session.
type GetSessionOptions struct {
	// RepositorySpecification is the repository specification for the session.
	RepositorySpecification *apiextensionsv1.JSON
	// OCMConfigurations is the list of OCM configurations to use for the session.
	OCMConfigurations []v1alpha1.OCMConfiguration
	// VerificationProvider is the optional additional provider that provides verification information for signatures.
	VerificationProvider v1alpha1.VerificationProvider
}

// GetSession returns an OCM context and session for the given options.
// Contexts are cached by configuration hash. Sessions are cached by (context hash, repo hash).
func (m *ContextCache) GetSession(opts *GetSessionOptions) (ocm.Context, ocm.Session, error) {
	if opts == nil {
		return nil, nil, fmt.Errorf("opts is nil")
	}
	if opts.RepositorySpecification == nil {
		return nil, nil, fmt.Errorf("repository spec is nil")
	}
	m.retrievalLock.Lock()
	defer m.retrievalLock.Unlock()

	// 1) Load config objects and hash them -> context key
	configObjs, err := m.getConfigObjects(opts.OCMConfigurations)
	if err != nil {
		return nil, nil, err
	}
	configHash, err := GetObjectDataHash(configObjs...)
	if err != nil {
		return nil, nil, fmt.Errorf("hash config objects: %w", err)
	}

	// 2) Get existing or build new context
	octx, err := m.getOrCreateContext(configHash, configObjs)
	if err != nil {
		return nil, nil, err
	}

	// 3) Always register verifications
	if err := m.registerVerifications(octx, opts); err != nil {
		return nil, nil, err
	}

	// 4) Get or build session
	key := sessionKey{
		ctxHash:  configHash,
		repoHash: hashBytesHex(opts.RepositorySpecification.Raw, sha256.New()),
	}
	sess := m.getOrCreateSession(key, octx)

	return octx, sess, nil
}

// --- helpers ---

type sessionKey struct {
	ctxHash  string
	repoHash string
}

func (m *ContextCache) getConfigObjects(configs []v1alpha1.OCMConfiguration) ([]ctrl.Object, error) {
	objs := make([]ctrl.Object, 0, len(configs))
	for _, c := range configs {
		var obj ctrl.Object
		switch c.Kind {
		case "Secret":
			obj = &corev1.Secret{}
		case "ConfigMap":
			obj = &corev1.ConfigMap{}
		default:
			return nil, fmt.Errorf("unsupported configuration kind: %s", c.Kind)
		}
		key := ctrl.ObjectKey{Namespace: c.Namespace, Name: c.Name}
		if err := m.lookupClient.Get(m.ctx, key, obj); err != nil {
			return nil, fmt.Errorf("get %s %s/%s: %w", c.Kind, c.Namespace, c.Name, err)
		}
		objs = append(objs, obj)
	}

	return objs, nil
}

func (m *ContextCache) getOrCreateContext(ctxHash string, configObjs []ctrl.Object) (ocm.Context, error) {
	if cached, ok := m.contexts.Get(ctxHash); ok {
		ctx := cached.(ocm.Context) //nolint:forcetypeassert // safe cast
		m.logger.V(1).Info("reusing cached ocm context", "hash", ctxHash, "id", ctx.GetId())

		return ctx, nil
	}
	m.logger.V(1).Info("creating new ocm context", "hash", ctxHash)

	octx := ocm.New(datacontext.MODE_EXTENDED)

	var applyErr error
	for _, obj := range configObjs {
		applyErr = errors.Join(applyErr, ConfigureContextForSecretOrConfigMap(m.ctx, octx, obj))
	}
	if applyErr != nil {
		return nil, applyErr
	}

	m.contexts.Add(ctxHash, octx)
	contextCacheSizeMetric.WithLabelValues(m.name).Inc()

	return octx, nil
}

func (m *ContextCache) registerVerifications(octx ocm.Context, opts *GetSessionOptions) error {
	if opts.VerificationProvider == nil {
		return nil
	}
	vs, err := GetVerifications(m.ctx, m.lookupClient, opts.VerificationProvider)
	if err != nil || len(vs) == 0 {
		return err
	}
	if reg := signingattr.Get(octx); reg != nil {
		for _, v := range vs {
			reg.RegisterPublicKey(v.Signature, v.PublicKey)
		}
	}

	return nil
}

func (m *ContextCache) getOrCreateSession(key sessionKey, octx ocm.Context) ocm.Session {
	if cached, ok := m.sessions.Get(key); ok {
		s := cached.(ocm.Session) //nolint:forcetypeassert // safe cast
		if !s.IsClosed() {
			m.logger.V(1).Info("reusing cached ocm session", "context", key.ctxHash, "repo", key.repoHash)

			return s
		}
		m.logger.V(1).Info("replacing close ocm session", "context", key.ctxHash, "repo", key.repoHash)

		// replace closed session with a fresh one
		s = ocm.NewSession(datacontext.NewSession())
		octx.Finalizer().Close(s)
		m.sessions.Add(key, s)

		return s
	}
	m.logger.V(1).Info("creating new ocm session", "context", key.ctxHash, "repo", key.repoHash)

	s := ocm.NewSession(datacontext.NewSession())
	octx.Finalizer().Close(s)
	m.sessions.Add(key, s)
	sessionCacheSizeMetric.WithLabelValues(m.name).Inc()

	return s
}

func hashBytesHex(b []byte, hash hash.Hash) string {
	if b == nil {
		return ""
	}

	return fmt.Sprintf("%x", hash.Sum(b))
}

func (m *ContextCache) Clear() {
	m.sessions.Clear()
	m.contexts.Clear()
}
