package main

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"time"
)

// adminHandler serves the pprof endpoints on their own mux. It never uses the default mux,
// so the public port cannot expose these paths by accident.
func adminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

// startAdmin starts the admin listener when addr is not empty. It returns nil when the listener is off.
// A serve error goes to errc. The caller must keep addr on a loopback or private interface.
func startAdmin(addr string, errc chan<- error) (*http.Server, error) {
	if addr == "" {
		return nil, nil
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	// No WriteTimeout: a CPU profile of 20 seconds must finish.
	srv := &http.Server{Handler: adminHandler(), ReadHeaderTimeout: 5 * time.Second}
	slog.Info("admin listening (pprof)", "addr", l.Addr().String())
	go func() {
		if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	return srv, nil
}
