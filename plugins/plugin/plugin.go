package plugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"ocm.software/open-component-model/plugins/manager"
)

type CleanupFunc func(ctx context.Context) error

type Plugin struct {
	Config manager.Config

	handlers    []Handler
	server      *http.Server
	cleanUpFunc CleanupFunc
	mu          sync.Mutex
	// this channel is none blocking because it's created with a capability of 1
	// and then immediately read by the idle checker.
	interrupt     chan bool
	workerCounter atomic.Int64
	logger        *slog.Logger
}

// NewPlugin creates a new Go based plugin. After creation,
// call RegisterHandlers to register the handlers responsible for this
// plugin's inner workings. A capabilities endpoint is automatically added
// to every plugin.
// TODO: Provide documentation for secure data flow with local certificate
// setup and certificate generation. At least start a document / issue.
func NewPlugin(logger *slog.Logger, conf manager.Config) *Plugin {
	l := logger
	if l == nil {
		l = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))
	}

	return &Plugin{
		Config:    conf,
		logger:    l,
		interrupt: make(chan bool, 1), // to not block any new work coming in
	}
}

func (p *Plugin) startIdleChecker() {
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
			p.logger.Info("idle check timer expired for plugin", "id", p.Config.ID)
			os.Exit(0)

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

		p.logger.Info("Received signal. Shutting down.", "signal", sig)

		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := p.GracefulShutdown(ctx); err != nil {
			p.logger.Error("Error shutting down plugin", "error", err)
		}
	}(ctx)

	return p.listen(ctx)
}

func (p *Plugin) Healthz(w http.ResponseWriter, _ *http.Request) {
	p.StartWork()
	defer p.StopWork()

	w.WriteHeader(http.StatusOK)
}

// listen starts listening for connections from the plugin manager.
func (p *Plugin) listen(ctx context.Context) error {
	p.logger.Info("Starting to listen at address", "type", p.Config.Type, "location", p.Config.Location)
	conn, err := net.Listen(string(p.Config.Type), p.Config.Location)
	if err != nil {
		return fmt.Errorf("failed to connect to socket from client: %w", err)
	}

	m := http.NewServeMux()
	for _, h := range p.handlers {
		m.HandleFunc(h.Location, h.Handler)
	}

	m.HandleFunc("/shutdown", p.Shutdown)
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
	go p.startIdleChecker()

	p.server = server

	return server.Serve(conn)
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
	case manager.Socket:
		p.logger.Info("Removing socket", "location", p.Config.Location)
		if err := os.Remove(p.Config.Location); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	if p.cleanUpFunc != nil {
		if err := p.cleanUpFunc(ctx); err != nil {
			return fmt.Errorf("failed to run custom cleanup function: %w", err)
		}
	}

	return nil
}

type Handler struct {
	Location string
	Handler  http.HandlerFunc
}

func (p *Plugin) RegisterHandlers(handlers ...Handler) error {
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

// RegisterCleanupFunc registers a function that is defined by the user and called
// during graceful shutdown. This function needs to deal with a context that will
// time out given a period of configured time.
func (p *Plugin) RegisterCleanupFunc(f CleanupFunc) {
	p.cleanUpFunc = f
}

func (p *Plugin) Shutdown(writer http.ResponseWriter, _ *http.Request) {
	p.logger.Info("Shutting down plugin", "id", p.Config.ID)
	writer.WriteHeader(http.StatusOK)
	if err := p.GracefulShutdown(context.Background()); err != nil {
		p.logger.Error("Error shutting down plugin", "error", err)
	}
}

type Error struct {
	Err    error `json:"error"`
	Status int   `json:"status"`
}

func NewError(err error, status int) *Error {
	return &Error{Err: err, Status: status}
}

func (e *Error) Write(w http.ResponseWriter) {
	http.Error(w, e.Err.Error(), e.Status)
}
