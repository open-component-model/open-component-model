package http

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

// Tests newDialer directly (white-box) because transport.DialContext is a
// method value bound to the dialer — once installed it cannot be inspected
// for its Timeout/KeepAlive without performing a real network dial.

func TestNewDialer(t *testing.T) {
	t.Run("only TCPDialTimeout — KeepAlive falls back to default", func(t *testing.T) {
		d := newDialer(&httpv1alpha1.TimeoutConfig{
			TCPDialTimeout: httpv1alpha1.NewTimeout(15 * time.Second),
		})
		assert.Equal(t, 15*time.Second, d.Timeout)
		assert.Equal(t, defaultKeepAlive, d.KeepAlive)
	})

	t.Run("only TCPKeepAlive — Timeout falls back to default", func(t *testing.T) {
		d := newDialer(&httpv1alpha1.TimeoutConfig{
			TCPKeepAlive: httpv1alpha1.NewTimeout(45 * time.Second),
		})
		assert.Equal(t, defaultDialTimeout, d.Timeout)
		assert.Equal(t, 45*time.Second, d.KeepAlive)
	})

	t.Run("negative TCPKeepAlive disables probes", func(t *testing.T) {
		d := newDialer(&httpv1alpha1.TimeoutConfig{
			TCPKeepAlive: httpv1alpha1.NewTimeout(-1 * time.Second),
		})
		assert.Equal(t, -1*time.Second, d.KeepAlive)
	})

	t.Run("both set — both override defaults", func(t *testing.T) {
		d := newDialer(&httpv1alpha1.TimeoutConfig{
			TCPDialTimeout: httpv1alpha1.NewTimeout(7 * time.Second),
			TCPKeepAlive:   httpv1alpha1.NewTimeout(8 * time.Second),
		})
		assert.Equal(t, 7*time.Second, d.Timeout)
		assert.Equal(t, 8*time.Second, d.KeepAlive)
	})
}
