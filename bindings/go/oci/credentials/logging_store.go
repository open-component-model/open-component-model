package credentials

import (
	"context"
	"log/slog"

	"oras.land/oras-go/v2/registry/remote/auth"
	remotecredentials "oras.land/oras-go/v2/registry/remote/credentials"
)

func wrapWithLogging(store remotecredentials.Store, base *slog.Logger) remotecredentials.Store {
	return &loggingStore{store, base}
}

type loggingStore struct {
	remotecredentials.Store
	base *slog.Logger
}

func (l *loggingStore) Get(ctx context.Context, serverAddress string) (auth.Credential, error) {
	logger := l.base.With("serverAddress", serverAddress)
	logger.DebugContext(ctx, "getting credentials")
	credential, err := l.Store.Get(ctx, serverAddress)

	switch {
	case err != nil:
		logger.ErrorContext(ctx, "failed to get credential", "error", err)
	case credential != auth.EmptyCredential:
		logger.DebugContext(ctx, "got credential", "username", credential.Username, "serverAddress", serverAddress)
	default:
		logger.DebugContext(ctx, "got no credential")
	}

	return credential, err
}
