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
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

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
	baseCtx       context.Context
	// this should be a logger using stderr instead of default logger.
	logger slog.Logger
}

// NewPlugin creates a new Go based plugin. After creation,
// call RegisterHandlers to register the handlers responsible for this
// plugin's inner workings. A capabilities endpoint is automatically added
// to every plugin. Takes an output device to print out the configure location
// for the plugin to so that the manager can pick it up.
// TODO(Skarlso): Provide documentation for secure data flow with local certificate
// setup and certificate generation. At least start a document / issue.
func NewPlugin(ctx context.Context, logger *slog.Logger, conf types.Config, output io.Writer) *Plugin {
	return &Plugin{
		Config:    conf,
		interrupt: make(chan bool, 1), // to not block any new work coming in
		output:    output,
		baseCtx:   ctx, // base context is used for graceful shutdown operation to finish properly
		logger:    *logger,
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

			if err := p.GracefulShutdown(ctx); err != nil {
				p.logger.ErrorContext(ctx, "failed to gracefully shutdown plugin", "error", err)
			}

			p.logger.InfoContext(ctx, "idle check timer expired for plugin", "id", p.Config.ID)
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

// Start starts the plugin and sets up a graceful shutdown catch for interrupts.
// The Context here is created in the plugin binary.
func (p *Plugin) Start(ctx context.Context) error {
	// Handle graceful shutdown on SIGINT/SIGTERM
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func(ctx context.Context) {
		sig := <-sigs

		p.logger.InfoContext(ctx, "Received signal. Shutting down.", "signal", sig)

		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := p.GracefulShutdown(ctx); err != nil {
			p.logger.ErrorContext(ctx, "Error shutting down plugin", "error", err)
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

	plugins.NewError(
		errors.New(
			"this endpoint may only be called with either HEAD or GET method"),
		http.StatusMethodNotAllowed).
		Write(w)
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
		m.HandleFunc(h.Location, p.panicRecovery(h.Handler))
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
	go p.startIdleChecker(ctx)

	p.server = server

	// output the location before starting the server
	var schemedLocation string
	switch p.Config.Type {
	case types.TCP:
		schemedLocation = loc // http is already included in tcp output
	case types.Socket:
		schemedLocation = "http+unix://" + loc
	}

	if _, err := fmt.Fprintln(p.output, schemedLocation); err != nil {
		return fmt.Errorf("failed to write location to output writer: %w", err)
	}

	return server.Serve(conn)
}

func (p *Plugin) panicRecovery(f func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				p.logger.ErrorContext(r.Context(), "panic recovered", "error", err)
				plugins.NewError(
					errors.New("panic recovered"),
					http.StatusInternalServerError).
					Write(w)
			}
		}()

		f(w, r)
	}
}

func (p *Plugin) determineLocation() (_ string, err error) {
	switch p.Config.Type {
	case types.Socket:
		loc := "/tmp/" + p.Config.ID + "-plugin.socket"
		if _, err := os.Stat(loc); err == nil {
			if cleanupErr := p.performCleanUp(loc); cleanupErr != nil {
				return "", fmt.Errorf("could not cleanup socket: %w", cleanupErr)
			}
		}

		if err := p.createLockFile(loc); err != nil {
			return "", fmt.Errorf("could not create lock file: %w", err)
		}

		return loc, nil
	case types.TCP:
		// Listen `:0` gives back a random _free_ port for the plugin to listen on.
		// Once we have this port, this listener is immediately closed and a purpose listener
		// will be opened with the specific port.
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
// In the case of sockets, it will remove the created socket.
func (p *Plugin) GracefulShutdown(ctx context.Context) error {
	p.logger.InfoContext(ctx, "Gracefully shutting down plugin", "id", p.Config.ID)
	// We ignore server closed errors because server closing might race with the listener.
	if err := p.server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}

	switch p.Config.Type {
	case types.Socket:
		p.logger.InfoContext(ctx, "removing socket", "location", p.location)
		if err := os.Remove(p.location); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		// also remove the lock file
		if err := os.Remove(p.location + ".lock"); err != nil && !errors.Is(err, os.ErrNotExist) {
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

// workerHandler will create a working handler. It will signal the plugin that it started to
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

// Shutdown is using attempting to gracefully shut down the server. Note that this endpoint
// needs to use baseContext that is provided during plugin creation instead of request context
// because otherwise, the shutdown is interrupted by the request context being cancelled mid-shutdown
// resulting in a context cancelled error instead of properly closing connection to the server.
func (p *Plugin) Shutdown(w http.ResponseWriter, _ *http.Request) {
	p.logger.InfoContext(p.baseCtx, "Shutting down plugin", "id", p.Config.ID)
	w.WriteHeader(http.StatusOK)
	if err := p.GracefulShutdown(p.baseCtx); err != nil {
		p.logger.ErrorContext(p.baseCtx, "Error shutting down plugin", "error", err)
	}
}

// performCleanUp looks for a lock file that contains the pid of the process using the corresponding socket file.
// if the process is not found, we remove the socket file AND the lock file.
func (p *Plugin) performCleanUp(loc string) error {
	lockFile := loc + ".lock"
	if _, err := os.Stat(lockFile); err != nil {
		return fmt.Errorf("lock file does not exists unable to determine using process: %w", err)
	}

	content, err := os.ReadFile(lockFile)
	if err != nil {
		return fmt.Errorf("could not read lock file: %w", err)
	}

	pid, err := strconv.Atoi(string(content))
	if err != nil {
		return fmt.Errorf("could not parse PID: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return p.removeFiles(loc, lockFile)
	}

	// Check if the process is actually running by sending syscall.SIGCONT signal to it.
	if err := process.Signal(syscall.SIGCONT); err != nil {
		// error means the process is gone; it's safe to remove the files.
		return p.removeFiles(loc, lockFile)
	}

	return fmt.Errorf("process using socket file is still alive under pid %d", process.Pid)
}

func (p *Plugin) removeFiles(loc string, lockFile string) error {
	// The Process doesn't exist, safe to clean up
	if err := os.Remove(lockFile); err != nil {
		return fmt.Errorf("could not remove lock file: %w", err)
	}
	if err := os.Remove(loc); err != nil {
		return fmt.Errorf("could not remove socket file: %w", err)
	}
	return nil
}

// createLockFile creates a lock file and puts the current process PID into the file.
func (p *Plugin) createLockFile(loc string) (err error) {
	lockFile := loc + ".lock"
	file, err := os.Create(lockFile)
	if err != nil {
		return fmt.Errorf("could not create lock file: %w", err)
	}

	defer func() {
		err = errors.Join(err, file.Close())
	}()

	pid := strconv.Itoa(os.Getpid())
	if _, err = file.Write([]byte(pid)); err != nil {
		return fmt.Errorf("could not write lock file: %w", err)
	}

	return nil
}
