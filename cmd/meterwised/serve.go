package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func cmdServe(ctx context.Context, e *env, args []string) int {
	asJSON, _, code := parseFlags(e, "serve", args)
	if code != exitOK {
		return code
	}
	cfg, err := config.Load(e.lookup)
	if err != nil {
		return fail(e, asJSON, contract.AsError(err), exitFailed)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.Listen)
	if err != nil {
		return fail(e, asJSON, contract.NewError(contract.CodeInternal, "cannot listen on "+cfg.Listen), exitFailed)
	}
	log := slog.New(slog.NewJSONHandler(e.stderr, &slog.HandlerOptions{Level: logLevel(cfg.LogLevel)}))
	ok(e, asJSON, map[string]string{"listening": ln.Addr().String()})
	log.Info("serving", "addr", ln.Addr().String(), "env", cfg.Env)
	if err := serveUntilDone(ctx, ln, newHandler()); err != nil {
		log.Error("serve failed", "err", err)
		return exitFailed
	}
	log.Info("stopped")
	return exitOK
}

func serveUntilDone(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: readHeaderTimeout}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// newHandler serves the public routes of the contract route table. A route in
// the table with no case here fails TestHandlerServesEveryPublicRoute.
func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, contract.Health{Status: "ok"})
	})
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, contract.Version{Version: version, Commit: commit})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		e := contract.NewError(contract.CodeNotFound, "no such route")
		writeJSON(w, e.Status(), contract.ErrorBody{Code: e.Code, Message: e.Message})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func logLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return l
}
