package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tutu-network/tutu/internal/api"
	"github.com/tutu-network/tutu/internal/infra/engine"
	"github.com/tutu-network/tutu/internal/infra/registry"
	"github.com/tutu-network/tutu/internal/infra/sqlite"
)

// Daemon is the core TuTu runtime. It wires together all services.
type Daemon struct {
	Config  Config
	DB      *sqlite.DB
	Models  *registry.Manager
	Pool    *engine.Pool
	Server  *api.Server
	cancel  context.CancelFunc
}

// New creates and initializes a Daemon with all services wired.
func New() (*Daemon, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	return NewWithConfig(cfg)
}

// NewWithConfig creates a Daemon with the given configuration.
func NewWithConfig(cfg Config) (*Daemon, error) {
	// Open SQLite
	db, err := sqlite.Open(tutuHome())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Initialize model manager
	modelsDir := cfg.Models.Dir
	if modelsDir == "" {
		modelsDir = filepath.Join(tutuHome(), "models")
	}
	mgr := registry.NewManager(modelsDir, db)

	// Initialize inference engine
	// Try real llama-server subprocess backend first, fall back to mock
	var backend engine.InferenceBackend
	realBackend, err := engine.NewSubprocessBackend(tutuHome())
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: llama-server not found, using mock backend (no real AI inference)\n")
		fmt.Fprintf(os.Stderr, "  Install llama-server for real model inference.\n")
		backend = engine.NewMockBackend()
	} else {
		backend = realBackend
	}
	pool := engine.NewPool(backend, parseStorageSize(cfg.Models.MaxStorage), mgr.Resolve)

	// Initialize API server
	srv := api.NewServer(pool, mgr)

	return &Daemon{
		Config: cfg,
		DB:     db,
		Models: mgr,
		Pool:   pool,
		Server: srv,
	}, nil
}

// Serve starts the HTTP server and blocks until shutdown.
func (d *Daemon) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	d.cancel = cancel

	// Start idle reaper in background
	go d.Pool.IdleReaper(ctx)

	addr := fmt.Sprintf("%s:%d", d.Config.API.Host, d.Config.API.Port)

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      d.Server.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute, // Long for streaming
		IdleTimeout:  2 * time.Minute,
	}

	// Graceful shutdown on signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigCh:
		case <-ctx.Done():
		}

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		_ = d.Pool.UnloadAll()
		_ = httpServer.Shutdown(shutdownCtx)
		_ = d.DB.Close()
	}()

	fmt.Printf("TuTu serving on http://%s\n", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Close shuts down all daemon resources.
func (d *Daemon) Close() {
	if d.cancel != nil {
		d.cancel()
	}
	if d.Pool != nil {
		_ = d.Pool.UnloadAll()
	}
	if d.DB != nil {
		_ = d.DB.Close()
	}
}

// parseStorageSize converts "50GB" to bytes. Simple parser for config.
func parseStorageSize(s string) uint64 {
	var val uint64
	var unit string
	fmt.Sscanf(s, "%d%s", &val, &unit)
	if val == 0 {
		return 50 * 1024 * 1024 * 1024 // Default 50GB
	}
	switch unit {
	case "TB":
		return val * 1024 * 1024 * 1024 * 1024
	case "GB":
		return val * 1024 * 1024 * 1024
	case "MB":
		return val * 1024 * 1024
	default:
		return val * 1024 * 1024 * 1024 // Assume GB
	}
}
