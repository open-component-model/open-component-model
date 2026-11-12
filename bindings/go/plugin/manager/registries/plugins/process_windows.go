package plugins

import (
	"context"
	"net/http"
	"time"
)

// interruptTimeout bounds the request asking a plugin to shut down. The plugin answers before shutting down.
const interruptTimeout = 5 * time.Second

// Interrupt asks the plugin to shut down gracefully through its shutdown endpoint, which the
// plugin SDK serves, because Windows cannot deliver os.Interrupt to another process.
func (p Process) Interrupt(ctx context.Context) error {
	// Like sending a signal, delivering the request must not fail because the caller's context is already done.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), interruptTimeout)
	defer cancel()

	return Call(ctx, p.client, p.typ, p.location, "shutdown", http.MethodGet)
}
