// Package api provides the HTTP server for TuTu.
// It exposes an OpenAI-compatible API (Phase 0) and an Ollama-compatible API.
package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/engine"
	"github.com/tutu-network/tutu/internal/infra/registry"
)

// Server is the TuTu HTTP API server.
type Server struct {
	pool           *engine.Pool
	models         *registry.Manager
	metricsEnabled bool
	mcpHandler     http.Handler   // Phase 2: MCP transport handler (nil if not set)
	engagement     *EngagementAPI // Phase 2: Engagement REST API
	earningsHub    *EarningsHub   // Phase 2: Live earnings SSE feed
}

// NewServer creates a new API server.
func NewServer(pool *engine.Pool, models *registry.Manager) *Server {
	return &Server{pool: pool, models: models}
}

// EnableMetrics enables the /metrics Prometheus endpoint.
func (s *Server) EnableMetrics() { s.metricsEnabled = true }

// SetMCPHandler sets the MCP Streamable HTTP transport handler.
func (s *Server) SetMCPHandler(h http.Handler) { s.mcpHandler = h }

// SetEngagement sets the engagement API services.
func (s *Server) SetEngagement(e *EngagementAPI) { s.engagement = e }

// SetEarningsHub sets the live earnings SSE hub.
func (s *Server) SetEarningsHub(h *EarningsHub) { s.earningsHub = h }

// EarningsHub returns the live earnings hub (for broadcasting events).
func (s *Server) EarningsHub() *EarningsHub { return s.earningsHub }

// Handler returns the chi router with all routes mounted.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(5 * time.Minute))
	r.Use(corsMiddleware)

	// Health check for Railway/Render
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "ok",
		})
	})

	// API status endpoint
	r.Get("/api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "TuTu is running",
		})
	})

	r.Get("/api/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"version": "0.1.0",
		})
	})

	// OpenAI-compatible endpoints (Phase 0)
	r.Route("/v1", func(r chi.Router) {
		r.Get("/models", s.handleListModels)
		r.Post("/chat/completions", s.handleChatCompletions)
		r.Post("/embeddings", s.handleEmbeddings)
	})

	// Ollama-compatible endpoints
	r.Route("/api", func(r chi.Router) {
		r.Post("/generate", s.handleOllamaGenerate)
		r.Post("/chat", s.handleOllamaChat)
		r.Get("/tags", s.handleOllamaTags)
		r.Post("/show", s.handleOllamaShow)
		r.Post("/pull", s.handleOllamaPull)
		r.Delete("/delete", s.handleOllamaDelete)
		r.Get("/ps", s.handleOllamaPs)
	})

	// Prometheus metrics endpoint (Phase 1 — observability)
	if s.metricsEnabled {
		r.Handle("/metrics", promhttp.Handler())
	}

	// MCP Streamable HTTP endpoint (Phase 2 — enterprise gateway)
	if s.mcpHandler != nil {
		r.Handle("/mcp", s.mcpHandler)
	}

	// Engagement API (Phase 2 — streaks, levels, achievements, quests, notifications)
	if s.engagement != nil {
		r.Route("/api/engagement", func(r chi.Router) {
			r.Get("/streak", s.engagement.HandleStreak)
			r.Get("/level", s.engagement.HandleLevel)
			r.Get("/achievements", s.engagement.HandleAchievements)
			r.Get("/quests", s.engagement.HandleQuests)
			r.Get("/notifications", s.engagement.HandleNotifications)
			r.Post("/notifications/{id}/shown", s.engagement.HandleNotificationShown)
			r.Get("/summary", s.engagement.HandleSummary)
		})
	}

	// Live earnings SSE feed (Phase 2 — Architecture Part XIII #5)
	if s.earningsHub != nil {
		r.Get("/api/earnings/live", s.earningsHub.HandleEarningsSSE)
	}

	// Serve static website files for all other routes
	websiteDir := findWebsiteDir()
	if websiteDir != "" {
		fileServer := http.FileServer(http.Dir(websiteDir))
		r.Handle("/*", fileServer)
	} else {
		// Fallback if website directory not found
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{
				"status": "TuTu is running",
			})
		})
	}

	return r
}

// findWebsiteDir locates the website directory in various contexts.
func findWebsiteDir() string {
	// Try common locations
	candidates := []string{
		"website",                    // Running from project root
		"../website",                 // Running from build dir
		"/app/website",               // Docker container
		filepath.Join(os.Getenv("TUTU_HOME"), "..", "..", "website"), // Via TUTU_HOME
	}
	
	for _, dir := range candidates {
		if stat, err := os.Stat(dir); err == nil && stat.IsDir() {
			// Check if index.html exists
			if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
				return dir
			}
		}
	}
	return ""
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"message": msg,
			"type":    "error",
		},
	})
}

// corsMiddleware adds CORS headers for local development.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ─── Shared types used across API formats ────────────────────────────────────

// We keep these unexported since they are only used within the api package.

func defaultLoadOpts() engine.LoadOptions {
	return engine.LoadOptions{
		NumGPULayers: -1,
		NumCtx:       4096,
	}
}

func defaultGenParams() engine.GenerateParams {
	return engine.GenerateParams{
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   2048,
	}
}

// modelToOpenAI converts a domain.ModelInfo to OpenAI model list entry.
func modelToOpenAI(m domain.ModelInfo) map[string]interface{} {
	return map[string]interface{}{
		"id":       m.Name,
		"object":   "model",
		"created":  m.PulledAt.Unix(),
		"owned_by": "tutu",
	}
}
