package daemon

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tutu-network/tutu/internal/api"
	"github.com/tutu-network/tutu/internal/app/credit"
	"github.com/tutu-network/tutu/internal/app/engagement"
	"github.com/tutu-network/tutu/internal/app/executor"
	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/health"
	"github.com/tutu-network/tutu/internal/infra/anomaly"
	"github.com/tutu-network/tutu/internal/infra/autoscale"
	"github.com/tutu-network/tutu/internal/infra/democracy"
	"github.com/tutu-network/tutu/internal/infra/engine"
	"github.com/tutu-network/tutu/internal/infra/federation"
	"github.com/tutu-network/tutu/internal/infra/finetune"
	"github.com/tutu-network/tutu/internal/infra/flywheel"
	"github.com/tutu-network/tutu/internal/infra/gossip"
	"github.com/tutu-network/tutu/internal/infra/governance"
	"github.com/tutu-network/tutu/internal/infra/healing"
	"github.com/tutu-network/tutu/internal/infra/intelligence"
	"github.com/tutu-network/tutu/internal/infra/marketplace"
	_ "github.com/tutu-network/tutu/internal/infra/metrics" // Register Prometheus metrics
	"github.com/tutu-network/tutu/internal/infra/mlscheduler"
	"github.com/tutu-network/tutu/internal/infra/network"
	"github.com/tutu-network/tutu/internal/infra/observability"
	"github.com/tutu-network/tutu/internal/infra/passive"
	"github.com/tutu-network/tutu/internal/infra/planetary"
	"github.com/tutu-network/tutu/internal/infra/region"
	"github.com/tutu-network/tutu/internal/infra/registry"
	"github.com/tutu-network/tutu/internal/infra/reputation"
	"github.com/tutu-network/tutu/internal/infra/resource"
	"github.com/tutu-network/tutu/internal/infra/scheduler"
	"github.com/tutu-network/tutu/internal/infra/selfheal"
	"github.com/tutu-network/tutu/internal/infra/sqlite"
	"github.com/tutu-network/tutu/internal/infra/universal"
	"github.com/tutu-network/tutu/internal/mcp"
	"github.com/tutu-network/tutu/internal/security"
)

// Daemon is the core TuTu runtime. It wires together all services.
type Daemon struct {
	Config Config
	DB     *sqlite.DB
	Models *registry.Manager
	Pool   *engine.Pool
	Server *api.Server
	cancel context.CancelFunc

	// Phase 1 components
	Idle     *resource.IdleDetector
	Governor *resource.Governor
	Gossip   *gossip.SWIM
	Fabric   *network.Fabric
	Executor *executor.Executor
	Health   *health.Checker
	Credit   *credit.Service
	Keypair  *security.Keypair

	// Phase 2 components
	Streak       *engagement.StreakService
	Level        *engagement.LevelService
	Achievement  *engagement.AchievementService
	Quest        *engagement.QuestService
	Notification *engagement.NotificationService
	MCPGateway   *mcp.Gateway
	MCPTransport *mcp.Transport
	MCPMeter     *mcp.Meter
	EarningsHub  *api.EarningsHub

	// Phase 3 components — multi-region, scheduling, self-healing, observability
	Router     *region.Router
	Scheduler  *scheduler.Scheduler
	Tracer     *observability.Tracer
	Breaker    *healing.CircuitBreaker
	Quarantine *healing.QuarantineManager
	Capacity   *passive.CapacityAdvertiser
	Prefetcher *passive.Prefetcher

	// Phase 4 components — planet scale, marketplace, fine-tuning
	FineTuneCoordinator *finetune.Coordinator
	Marketplace         *marketplace.Store

	// Phase 5 components — federation, governance, reputation, anomaly
	Federation *federation.Registry
	Governance *governance.Engine
	Reputation *reputation.Tracker
	Anomaly    *anomaly.Detector

	// Phase 6 components — singularity: self-organizing network
	MLScheduler  *mlscheduler.Scheduler
	AutoScaler   *autoscale.Scaler
	SelfHeal     *selfheal.Mesh
	Intelligence *intelligence.Optimizer

	// Phase 7 components — event horizon: world's largest
	Planetary *planetary.TopologyManager
	Access    *universal.AccessManager
	Flywheel  *flywheel.Tracker
	Democracy *democracy.Engine
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
	// Try real llama-server subprocess backend first
	// If not found, auto-download it from llama.cpp releases
	var backend engine.InferenceBackend
	realBackend, err := engine.NewSubprocessBackend(tutuHome())
	if err != nil {
		// llama-server not found — try to auto-download it
		fmt.Fprintf(os.Stderr, "  llama-server not found — downloading automatically...\n")
		llamaPath, dlErr := engine.DownloadLlamaServer(tutuHome(), func(status string, pct float64) {
			// Use simple line-based output that works on all terminals (no ANSI codes)
			fmt.Fprintf(os.Stderr, "\r  %-70s", status)
		})
		if dlErr != nil {
			fmt.Fprintf(os.Stderr, "\n")
			fmt.Fprintf(os.Stderr, "  WARNING: could not auto-download llama-server: %v\n", dlErr)
			fmt.Fprintf(os.Stderr, "  Using mock backend (no real AI inference).\n")
			fmt.Fprintf(os.Stderr, "  To fix: install llama-server manually — see https://github.com/ggml-org/llama.cpp/releases\n")
			backend = engine.NewMockBackend()
		} else {
			fmt.Fprintf(os.Stderr, "\n")
			_ = llamaPath
			// Retry with the downloaded binary
			realBackend, err = engine.NewSubprocessBackend(tutuHome())
			if err != nil {
				fmt.Fprintf(os.Stderr, "  WARNING: downloaded but cannot use llama-server: %v\n", err)
				backend = engine.NewMockBackend()
			} else {
				backend = realBackend
			}
		}
	} else {
		backend = realBackend
	}

	// Wire up progress callback for model loading feedback
	if sb, ok := backend.(*engine.SubprocessBackend); ok {
		sb.SetProgress(func(msg string) {
			fmt.Fprintf(os.Stderr, "\r  %-70s", msg)
		})
	}

	pool := engine.NewPool(backend, parseStorageSize(cfg.Models.MaxStorage), mgr.Resolve)

	// Initialize API server
	srv := api.NewServer(pool, mgr)

	// Enable Prometheus /metrics if configured
	if cfg.Telemetry.Prometheus {
		srv.EnableMetrics()
	}

	d := &Daemon{
		Config: cfg,
		DB:     db,
		Models: mgr,
		Pool:   pool,
		Server: srv,
	}

	// ─── Phase 1 components ────────────────────────────────────────────

	// Crypto identity (Ed25519)
	kp, err := security.LoadOrCreateKeypair(tutuHome())
	if err != nil {
		log.Printf("[daemon] WARNING: failed to load keypair: %v (gossip signing disabled)", err)
	}
	d.Keypair = kp

	// Derive node ID from public key (first 16 hex chars) if not configured
	nodeID := cfg.Node.ID
	if nodeID == "" && kp != nil {
		hex := kp.PublicKeyHex()
		if len(hex) > 16 {
			nodeID = "node-" + hex[:16]
		}
	}
	if nodeID == "" {
		nodeID = "node-local"
	}

	// Idle detector
	d.Idle = resource.NewIdleDetector()

	// Resource governor (creates its own idle detector, thermal, battery monitors)
	govCfg := resource.GovernorConfig{
		ThermalThrottle: cfg.Resources.ThermalThrottle,
		ThermalShutdown: cfg.Resources.ThermalShutdown,
		BatteryMinPct:   20, // From architecture spec
		TickInterval:    5 * time.Second,
	}
	d.Governor = resource.NewGovernor(govCfg)

	// Credit service
	d.Credit = credit.NewService(db)

	// SWIM gossip (created by fabric internally, but kept for direct access)
	gossipCfg := gossip.DefaultConfig()

	// Network fabric
	fabricCfg := network.FabricConfig{
		Enabled:           cfg.Network.Enabled,
		CloudCoreEndpoint: cfg.Network.CloudCore,
		HeartbeatInterval: parseDuration(cfg.Network.HeartbeatInterval, 10*time.Second),
		Region:            cfg.Node.Region,
		GossipConfig:      gossipCfg,
	}
	if kp != nil {
		d.Fabric = network.NewFabric(fabricCfg, kp, d.Governor)
	}

	// Task executor
	execCfg := executor.Config{
		MaxConcurrent: cfg.API.MaxConcurrent,
	}
	if execCfg.MaxConcurrent == 0 {
		execCfg.MaxConcurrent = 4
	}
	d.Executor = executor.New(execCfg, d.Governor, db)

	// Health checker
	d.Health = health.NewChecker(db, modelsDir)

	// ─── Phase 2 components ────────────────────────────────────────────

	// Engagement engine
	d.Streak = engagement.NewStreakService(db)
	d.Level = engagement.NewLevelService(db)
	d.Achievement = engagement.NewAchievementService(db)
	d.Quest = engagement.NewQuestService(db)
	d.Notification = engagement.NewNotificationService(db)

	// MCP Gateway
	slaEngine := mcp.NewSLAEngine()
	d.MCPMeter = mcp.NewMeter(slaEngine)
	d.MCPGateway = mcp.NewGateway(slaEngine, d.MCPMeter)
	d.MCPTransport = mcp.NewTransport(d.MCPGateway)

	// Mount MCP endpoint on the API server
	srv.SetMCPHandler(d.MCPTransport)

	// Engagement REST API
	engAPI := &api.EngagementAPI{
		Streak:       d.Streak,
		Level:        d.Level,
		Achievement:  d.Achievement,
		Quest:        d.Quest,
		Notification: d.Notification,
	}
	srv.SetEngagement(engAPI)

	// Live earnings SSE hub
	d.EarningsHub = api.NewEarningsHub()
	srv.SetEarningsHub(d.EarningsHub)

	// ─── Phase 3 components ────────────────────────────────────────────

	// Multi-region router — routes tasks to optimal region
	localRegion := domain.RegionID(cfg.Node.Region)
	if !localRegion.IsValid() {
		localRegion = domain.RegionUSEast // default
	}
	routerCfg := region.DefaultConfig()
	routerCfg.LocalRegion = localRegion
	d.Router = region.NewRouter(routerCfg)

	// Advanced scheduler — work stealing, back-pressure, preemption
	d.Scheduler = scheduler.NewScheduler(scheduler.DefaultConfig())

	// Distributed tracing (ring buffer)
	d.Tracer = observability.NewTracer(observability.DefaultTracerConfig())

	// Self-healing — circuit breaker for Cloud Core calls
	d.Breaker = healing.NewCircuitBreaker("cloud-core", healing.DefaultCircuitBreakerConfig())
	d.Quarantine = healing.NewQuarantineManager(healing.DefaultQuarantineConfig())

	// Passive income — advertise capacity when idle
	hwTier := passive.ClassifyHardware(0, 0) // Detect at startup; re-classified when sensors report
	d.Capacity = passive.NewCapacityAdvertiser(hwTier)
	d.Prefetcher = passive.NewPrefetcher(5) // Pre-cache top 5 models

	// ─── Phase 4 components ────────────────────────────────────────────

	// Distributed fine-tuning coordinator
	d.FineTuneCoordinator = finetune.NewCoordinator(finetune.DefaultCoordinatorConfig())

	// Model marketplace
	d.Marketplace = marketplace.NewStore(marketplace.DefaultStoreConfig())

	// ─── Phase 5 components ────────────────────────────────────────────

	// Federation registry — private sub-networks for organizations
	d.Federation = federation.NewRegistry(federation.DefaultRegistryConfig())

	// Governance engine — credit-weighted voting on network parameters
	d.Governance = governance.NewEngine(governance.DefaultEngineConfig())

	// Reputation tracker — EMA-based trust scoring for nodes
	d.Reputation = reputation.NewTracker(reputation.DefaultTrackerConfig())

	// Anomaly detector — behavioral profiling + statistical outlier detection
	d.Anomaly = anomaly.NewDetector(anomaly.DefaultDetectorConfig())

	// ─── Phase 6 components ────────────────────────────────────────────

	// ML-driven scheduler — UCB1 multi-armed bandit for optimal node assignment
	d.MLScheduler = mlscheduler.NewScheduler(mlscheduler.DefaultConfig())

	// Predictive auto-scaler — exponential smoothing + seasonal forecasting
	d.AutoScaler = autoscale.NewScaler(autoscale.DefaultConfig())

	// Self-healing mesh — autonomous incident response with runbooks
	d.SelfHeal = selfheal.NewMesh(selfheal.DefaultConfig())

	// Network intelligence — model placement optimization + retirement
	d.Intelligence = intelligence.NewOptimizer(intelligence.DefaultConfig())

	// ─── Phase 7 components ────────────────────────────────────────────

	// Planetary-scale topology — continental mesh routing, model distribution
	d.Planetary = planetary.NewTopologyManager(planetary.DefaultConfig())

	// Universal access — free/education/pro/enterprise tier enforcement
	d.Access = universal.NewAccessManager(universal.DefaultConfig())

	// Economic flywheel — self-sustaining economy health monitoring
	d.Flywheel = flywheel.NewTracker(flywheel.DefaultConfig())

	// AI democracy — community governance for all network parameters
	d.Democracy = democracy.NewEngine(democracy.DefaultConfig())

	return d, nil
}

// Serve starts the HTTP server and blocks until shutdown.
func (d *Daemon) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	d.cancel = cancel

	// Start idle reaper in background
	go d.Pool.IdleReaper(ctx)

	// ─── Phase 1: Start background services ────────────────────────────

	// Health checker (always runs)
	go d.Health.Run(ctx)

	// Network fabric (if enabled)
	if d.Config.Network.Enabled {
		go func() {
			if err := d.Fabric.Start(ctx); err != nil {
				log.Printf("[daemon] fabric start error: %v", err)
			}
		}()
	}

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

		// Stop Phase 1 components
		if d.Fabric != nil {
			d.Fabric.Stop()
		}

		_ = d.Pool.UnloadAll()
		_ = httpServer.Shutdown(shutdownCtx)
		_ = d.DB.Close()
	}()

	fmt.Printf("TuTu serving on http://%s\n", addr)
	if d.Config.Network.Enabled {
		fmt.Printf("  Network: enabled (Cloud Core: %s)\n", d.Config.Network.CloudCore)
	}
	if d.Config.Telemetry.Prometheus {
		fmt.Printf("  Metrics: http://%s/metrics\n", addr)
	}

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
	if d.Fabric != nil {
		d.Fabric.Stop()
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

// parseDuration parses a duration string, returning a fallback on error.
func parseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
