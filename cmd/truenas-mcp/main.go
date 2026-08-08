// Command truenas-mcp serves the TrueNAS MCP server.
//
// It is configured entirely through environment variables and refuses to start
// on invalid configuration rather than running degraded, because its deployment
// target is an appliance whose app UI reports failures poorly.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cedricziel/truenas-mcp/internal/config"
	"github.com/cedricziel/truenas-mcp/internal/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// version is set at build time via -ldflags and records the source commit.
var version = "dev"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	healthcheck := flag.Bool("healthcheck", false,
		"probe the local health endpoint and exit; used as the container health check")
	stdio := flag.Bool("stdio", false,
		"serve MCP over stdin/stdout instead of HTTP, for clients that spawn this process directly")
	flag.Parse()

	if err := flagConflictErr(*stdio, *healthcheck); err != nil {
		log.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	mode := config.ModeHTTP
	if *stdio {
		mode = config.ModeStdio
	}

	cfg, err := config.Load(os.Getenv, mode)
	if err != nil {
		log.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	// Runs inside the container as its health check, since the distroless
	// image carries no shell or HTTP client.
	if *healthcheck {
		if err := server.Probe(server.ProbeURL(cfg.Listen)); err != nil {
			log.Error("health probe failed", "error", err)
			os.Exit(1)
		}
		return
	}

	log.Info("starting truenas-mcp", "version", version, "config", cfg.Summary())
	for _, w := range cfg.Warnings() {
		log.Warn(w)
	}

	if *stdio {
		if err := runStdio(context.Background(), cfg, log); err != nil {
			log.Error("server stopped", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := run(context.Background(), cfg, log); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// flagConflictErr rejects flag combinations that can only ever fail, so the
// operator gets the real reason rather than a downstream failure that names
// the wrong problem. --healthcheck probes cfg.Listen, a TCP listener stdio
// mode never starts, so the two cannot be combined.
func flagConflictErr(stdio, healthcheck bool) error {
	if stdio && healthcheck {
		return errors.New(
			"--stdio and --healthcheck cannot be combined: --healthcheck probes the HTTP listener, " +
				"which stdio mode never starts")
	}
	return nil
}

// runStdio serves MCP over stdin/stdout under the single API key the
// operator configured. Unlike the HTTP transport, which multiplexes many
// callers each under their own credential, a stdio process is spawned by one
// client for one user, so one provider for that one credential is enough.
func runStdio(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	sessions := server.NewSessionManager(cfg.TargetURL(), cfg.TargetInsecureSkipVerify)
	defer sessions.CloseAll()

	// The provider opens lazily and reconnects a dead session -- the same
	// logic the HTTP transport uses per caller, see NewSessionProvider. A
	// stdio client's only feedback channel is this process's stderr and exit
	// code, so a bad API key should fail loudly at spawn time rather than on
	// the first tool call: calling the provider once here does that. Every
	// later call, including reconnects, goes through this same provider.
	session := server.NewSessionProvider(sessions, "stdio", cfg.APIKey)
	if _, err := session(ctx); err != nil {
		return fmt.Errorf("connecting to %s: %w", cfg.Target, err)
	}

	err := serveStdio(ctx, server.MCPConfig{
		Version:      version,
		Target:       cfg.Target,
		EnableWrites: cfg.EnableWrites,
	}, session, &mcp.StdioTransport{})

	// ctx is only cancelled by the signal handler above (or its parent), so
	// this distinguishes a SIGINT/SIGTERM shutdown from the client simply
	// closing the connection.
	if ctx.Err() != nil {
		log.Info("shutting down")
	}
	return shutdownErr(err)
}

// serveStdio builds the MCP server and runs it on transport. Split out from
// runStdio so tests can substitute an in-memory transport and a stub session
// in place of a real stdin/stdout pipe and a live TrueNAS connection.
func serveStdio(ctx context.Context, cfg server.MCPConfig, session func(context.Context) (*server.Session, error), transport mcp.Transport) error {
	srv := server.NewMCPServer(cfg, session)
	return srv.Run(ctx, transport)
}

// shutdownErr turns the context-cancellation error srv.Run reports on a
// signal-driven shutdown into a clean nil: SIGINT/SIGTERM ending the process
// is intentional, not a failure worth a non-zero exit.
func shutdownErr(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func run(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	health := server.NewHealth(version)

	// Each caller's tools run on a connection authenticated with that caller's
	// own API key. The server stores none.
	sessions := server.NewSessionManager(cfg.TargetURL(), cfg.TargetInsecureSkipVerify)
	defer sessions.CloseAll()

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health)
	mux.Handle("/mcp", server.NewMCPHandler(server.MCPConfig{
		Version:      version,
		Target:       cfg.Target,
		EnableWrites: cfg.EnableWrites,
		Sessions:     sessions,
	}))

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
