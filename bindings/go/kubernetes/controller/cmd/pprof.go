//go:build pprof

package main

import (
	"errors"
	"net/http"
	"net/http/pprof"
	"time"
)

const pprofEnabled = true

// startPprof serves the profiling endpoints on its own listener so that the
// profiles stay off the metrics server and out of non-debug builds entirely.
func startPprof(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second, //nolint:mnd // no magic number
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			setupLog.Error(err, "pprof server failed")
		}
	}()
}
