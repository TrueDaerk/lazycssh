package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
)

// startPprof serves net/http/pprof on addr for the lifetime of the process.
// It is a development hook: profiling a live run is the only way to see where
// a real fleet spends its cycles, and a TUI has no stdout to print counters
// on. The server binds before the TUI starts so a bad address fails readably.
//
// The handlers go on a private mux, not http.DefaultServeMux, so nothing else
// in the process can accidentally publish itself alongside the profiles.
func startPprof(addr string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("pprof listen on %s: %w", addr, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	go func() {
		// The server lives as long as the process; an error here means the
		// listener died, and losing the profiling hook must not kill a run.
		_ = http.Serve(ln, mux)
	}()

	return ln.Addr().String(), nil
}
