package sdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// OutputFormat defines an unequivocal format in which to report back the location of the plugin startup.
// The format is as follows: ConnectionType|Location. For example: unix|/tmp/plugin.socket or tcp|127.0.0.1:12345
// The line-break at the end is important, because of how the text scanner works per line.
var OutputFormat = "%s|%s\n"

// Plugin contains configuration for a single plugin and further details for life-cycle management.
// These include the server that's running the plugin, the handlers which serve functionality, and tracking idle time.
// Idle time tracks server work. If the server is not doing anything and not processing current requests and not getting
// new requests it will shut down automatically after a configured amount of time. If a new request comes in
// it will reset this timer.
type Plugin struct {
	Config types.Config

	handlers      []endpoints.Handler
	server        *http.Server
	interrupt     chan bool
	workerCounter atomic.Int64
	location      string
	output        io.Writer
}

// NewPlugin creates a new Go based plugin. After creation,
// call RegisterHandlers to register the handlers responsible for this
// plugin's inner workings. A capabilities endpoint is automatically added
// to every plugin. Takes an output device to print out the configure location
// for the plugin to so that the manager can pick it up.
// TODO(Skarlso): Provide documentation for secure data flow with local certificate
// setup and certificate generation. At least start a document / issue.
func NewPlugin(conf types.Config, output io.Writer) *Plugin {
	return &Plugin{
		Config:    conf,
		interrupt: make(chan bool, 1), // to not block any new work coming in
		output:    output,
	}
}

func (p *Plugin) startIdleChecker(ctx context.Context) {
	interval := time.Hour
	if p.Config.IdleTimeout != nil {
		interval = *p.Config.IdleTimeout
	}

	timer := time.NewTimer(interval)

	for {
		select {
		case <-timer.C:
			timer.Stop()

			_ = p.GracefulShutdown(context.Background())
			slog.InfoContext(ctx, "idle check timer expired for plugin", "id", p.Config.ID)
			return
		case working := <-p.interrupt:
			if !working && p.workerCounter.Load() == 0 {
				// no longer working, start the idle timeout
				timer.Stop()
				timer.Reset(interval)
			} else {
				// we received work, stop the timer.
				timer.Stop()
			}
		}
	}
}

func (p *Plugin) StartWork() {
	p.interrupt <- true
	p.workerCounter.Add(1)
}

func (p *Plugin) StopWork() {
	p.interrupt <- false
	p.workerCounter.Add(-1)
}

func (p *Plugin) Start(ctx context.Context) error {
	// Handle graceful shutdown on SIGINT/SIGTERM
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func(ctx context.Context) {
		sig := <-sigs

		slog.InfoContext(ctx, "Received signal. Shutting down.", "signal", sig)

		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := p.GracefulShutdown(ctx); err != nil {
			slog.ErrorContext(ctx, "Error shutting down plugin", "error", err)
		}
	}(ctx)

	return p.listen(ctx)
}

func (p *Plugin) Healthz(w http.ResponseWriter, r *http.Request) {
	p.StartWork()
	defer p.StopWork()

	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusMethodNotAllowed)
}

// listen starts listening for connections from the plugin manager.
func (p *Plugin) listen(ctx context.Context) error {
	loc, err := p.determineLocation()
	if err != nil {
		return fmt.Errorf("could not determine location: %w", err)
	}
	p.location = loc

	conn, err := net.Listen(string(p.Config.Type), loc)
	if err != nil {
		return fmt.Errorf("failed to connect to socket from client: %w", err)
	}

	m := http.NewServeMux()
	for _, h := range p.handlers {
		m.HandleFunc(h.Location, h.Handler)
	}

	m.HandleFunc("/shutdown", p.Shutdown(ctx))
	m.HandleFunc("/healthz", p.Healthz)

	server := &http.Server{
		Handler:           m,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		BaseContext: func(listener net.Listener) context.Context {
			return ctx
		},
	}

	// start idle checker.
	go p.startIdleChecker(ctx)

	p.server = server

	// output the location before starting the server
	if _, err := fmt.Fprintf(p.output, OutputFormat, p.Config.Type, loc); err != nil {
		return fmt.Errorf("failed to write location to output writer: %w", err)
	}

	return server.Serve(conn)
}

func (p *Plugin) determineLocation() (_ string, err error) {
	switch p.Config.Type {
	case types.Socket:
		loc := "/tmp/" + p.Config.ID + "-plugin.socket"
		if _, err := os.Stat(loc); err == nil {
			return "", fmt.Errorf("plugin location already exists: %s", loc)
		}

		return loc, nil
	case types.TCP:
		loc, err := net.Listen("tcp", ":0") //nolint: gosec // G102: only does it temporarily to find an empty address
		if err != nil {
			return "", fmt.Errorf("failed to start tcp listener: %w", err)
		}

		// Close the listener and return the address to be specific.
		defer func() {
			err = errors.Join(err, loc.Close())
		}()

		return loc.Addr().String(), nil
	}

	return "", fmt.Errorf("unknown plugin type: %s", p.Config.Type)
}

// GracefulShutdown will stop the server and do cleanup if necessary.
// In case of sockets it will remove the created socket.
func (p *Plugin) GracefulShutdown(ctx context.Context) error {
	slog.InfoContext(ctx, "Gracefully shutting down plugin", "id", p.Config.ID)
	// We ignore server closed errors because server closing might race with the listener.
	if err := p.server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}

	switch p.Config.Type {
	case types.Socket:
		slog.InfoContext(ctx, "removing socket", "location", p.location)
		if err := os.Remove(p.location); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	case types.TCP:
		// empty case for now
	}

	return nil
}

func (p *Plugin) RegisterHandlers(handlers ...endpoints.Handler) error {
	for _, h := range handlers {
		if h.Handler == nil {
			return fmt.Errorf("handler for %s is required", h.Location)
		}

		h.Handler = p.workerHandler(h.Handler)
		p.handlers = append(p.handlers, h)
	}

	return nil
}

// CreateHandler will create a working handler. It will signal the plugin that it started to
// work on something and set the plugin to working. This is important, because the plugin is
// constantly checking that if it's idle and hasn't heard from the manager in a set time
// it will exit. As soon as it gets a signal that it is doing something its internal check
// will be restarted once it's no longer doing anything.
func (p *Plugin) workerHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p.StartWork()
		defer p.StopWork()

		h(w, r)
	}
}

func (p *Plugin) Shutdown(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slog.InfoContext(ctx, "Shutting down plugin", "id", p.Config.ID)
		w.WriteHeader(http.StatusOK)
		if err := p.GracefulShutdown(context.Background()); err != nil {
			slog.ErrorContext(ctx, "Error shutting down plugin", "error", err)
		}
	}
}
