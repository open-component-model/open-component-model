package credentials

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"oras.land/oras-go/v2/registry/remote/auth"
)

// mockStore implements remotecredentials.Store for testing
type mockStore struct {
	credential auth.Credential
	err        error
}

func (m *mockStore) Get(ctx context.Context, serverAddress string) (auth.Credential, error) {
	return m.credential, m.err
}

func (m *mockStore) Delete(ctx context.Context, serverAddress string) error {
	return nil
}

func (m *mockStore) Put(ctx context.Context, serverAddress string, credential auth.Credential) error {
	return nil
}

func TestLoggingStore(t *testing.T) {
	tests := []struct {
		name           string
		mockCredential auth.Credential
		mockErr        error
		wantLogs       []string
	}{
		{
			name:           "successful credential retrieval",
			mockCredential: auth.Credential{Username: "testuser", Password: "testpass"},
			mockErr:        nil,
			wantLogs: []string{
				"level=DEBUG msg=\"getting credentials\" serverAddress=test-server",
				"level=DEBUG msg=\"got credential\" serverAddress=test-server username=testuser serverAddress=test-server",
			},
		},
		{
			name:           "empty credential retrieval",
			mockCredential: auth.EmptyCredential,
			mockErr:        nil,
			wantLogs: []string{
				"level=DEBUG msg=\"getting credentials\" serverAddress=test-server",
				"level=DEBUG msg=\"got no credential\" serverAddress=test-server",
			},
		},
		{
			name:           "error during retrieval",
			mockCredential: auth.EmptyCredential,
			mockErr:        errors.New("test error"),
			wantLogs: []string{
				"level=DEBUG msg=\"getting credentials\" serverAddress=test-server",
				"level=ERROR msg=\"failed to get credential\" serverAddress=test-server error=\"test error\"",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock store
			mock := &mockStore{
				credential: tt.mockCredential,
				err:        tt.mockErr,
			}

			// Create buffer and logger
			var buf bytes.Buffer
			handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if a.Key == "time" {
					return slog.Attr{}
				}
				return a
			}})
			logger := slog.New(handler)

			// Create logging store
			store := wrapWithLogging(mock, logger)

			// Test Get
			cred, err := store.Get(context.Background(), "test-server")

			// Verify credential and error
			if err != tt.mockErr {
				t.Errorf("expected error %v, got %v", tt.mockErr, err)
			}
			if cred != tt.mockCredential {
				t.Errorf("expected credential %v, got %v", tt.mockCredential, cred)
			}

			// Get log output and split into lines
			logOutput := buf.String()
			logLines := strings.Split(strings.TrimSpace(logOutput), "\n")

			// Verify log messages
			if len(logLines) != len(tt.wantLogs) {
				t.Errorf("expected %d log lines, got %d\nLog output:\n%s", len(tt.wantLogs), len(logLines), logOutput)
			}
			for i, wantLog := range tt.wantLogs {
				if i >= len(logLines) {
					t.Errorf("missing log line %d: expected %q", i, wantLog)
					continue
				}
				if logLines[i] != wantLog {
					t.Errorf("log line %d:\nexpected: %q\ngot:      %q", i, wantLog, logLines[i])
				}
			}
		})
	}
}
