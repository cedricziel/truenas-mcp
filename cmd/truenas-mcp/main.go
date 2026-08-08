// Command truenas-mcp serves the TrueNAS MCP server.
//
// It is configured entirely through environment variables and refuses to start
// on invalid configuration rather than running degraded, because its deployment
// target is an appliance whose app UI reports failures poorly.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cedricziel/truenas-mcp/internal/config"
	"github.com/cedricziel/truenas-mcp/internal/server"
)

// version is set at build time via -ldflags and records the source commit.
var version = "dev"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	log.Info("starting truenas-mcp", "version", version, "config", cfg.Summary())
	for _, w := range cfg.Warnings() {
		log.Warn(w)
	}

	if err := run(context.Background(), cfg, log); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	health := server.NewHealth(version)

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		var err error
		if cfg.TLSEnabled() {
			err = srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			err = srv.ListenAndServe()
		}
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errc <- err
	}()

	health.SetServing(true)
	log.Info("listening", "addr", cfg.Listen, "tls", cfg.TLSEnabled())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		health.SetUnhealthy("shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-errc
	}
}
