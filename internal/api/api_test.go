package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tutu-network/tutu/internal/infra/engine"
	"github.com/tutu-network/tutu/internal/infra/registry"
	"github.com/tutu-network/tutu/internal/infra/sqlite"
	"os"
	"path/filepath"
)

func newTestServer(t *testing.T) (*Server, func()) {
	t.Helper()
	dir := t.TempDir()

	// Setup DB
	dbDir := filepath.Join(dir, "db")
	db, err := sqlite.Open(dbDir)
	if err != nil {
		t.Fatalf("Open db: %v", err)
	}

	// Local HTTP server serving fake GGUF content (tests never hit network)
	fakeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("GGUF-FAKE-" + r.URL.Path))
	}))

	// Setup models
	modelsDir := filepath.Join(dir, "models")
	mgr := registry.NewManager(modelsDir, db)
	mgr.SetTestURL(fakeSrv.URL)

	// Setup engine pool (mock backend for unit tests)
	backend := engine.NewMockBackend()
	pool := engine.NewPool(backend, 1024*1024*1024, mgr.Resolve)

	srv := NewServer(pool, mgr)

	cleanup := func() {
		_ = pool.UnloadAll()
		_ = db.Close()
		fakeSrv.Close()
	}

	return srv, cleanup
}

func setupModel(t *testing.T, mgr *registry.Manager, name string) {
	t.Helper()
	if err := mgr.Pull(name, nil); err != nil {
		t.Fatalf("Pull(%s): %v", name, err)
	}
}

// newTestMgr creates a Manager with a local HTTP server (no network calls).
func newTestMgr(t *testing.T) (*registry.Manager, *sqlite.DB) {
	t.Helper()
	dir := t.TempDir()
	dbDir := filepath.Join(dir, "db")
	db, err := sqlite.Open(dbDir)
	if err != nil {
		t.Fatalf("Open db: %v", err)
	}

	fakeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("GGUF-FAKE-" + r.URL.Path))
	}))
	t.Cleanup(fakeSrv.Close)

	modelsDir := filepath.Join(dir, "models")
	mgr := registry.NewManager(modelsDir, db)
	mgr.SetTestURL(fakeSrv.URL)
	return mgr, db
}

// ─── Health Check ───────────────────────────────────────────────────────────

func TestAPI_Health(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["status"] != "TuTu is running" {
		t.Errorf("status = %q, unexpected", body["status"])
	}
}

func TestAPI_Version(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/version", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// ─── OpenAI /v1/models ──────────────────────────────────────────────────────

func TestAPI_ListModels_Empty(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if body["object"] != "list" {
		t.Errorf("object = %q, want \"list\"", body["object"])
	}
}

func TestAPI_ListModels_WithModels(t *testing.T) {
	mgr, db := newTestMgr(t)
	defer db.Close()
	setupModel(t, mgr, "llama3")
	setupModel(t, mgr, "mistral")

	backend := engine.NewMockBackend()
	pool := engine.NewPool(backend, 1024*1024*1024, mgr.Resolve)
	defer pool.UnloadAll()

	srv := NewServer(pool, mgr)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)

	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("data should be an array")
	}
	if len(data) != 2 {
		t.Errorf("len(data) = %d, want 2", len(data))
	}
}

// ─── OpenAI /v1/chat/completions ────────────────────────────────────────────

func TestAPI_ChatCompletions_NonStreaming(t *testing.T) {
	mgr, db := newTestMgr(t)
	defer db.Close()
	setupModel(t, mgr, "test-model")

	backend := engine.NewMockBackend()
	pool := engine.NewPool(backend, 1024*1024*1024, mgr.Resolve)
	defer pool.UnloadAll()

	srv := NewServer(pool, mgr)

	body := `{
		"model": "test-model",
		"messages": [{"role": "user", "content": "Hello"}],
		"stream": false
	}`

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["object"] != "chat.completion" {
		t.Errorf("object = %q, want \"chat.completion\"", resp["object"])
	}

	choices, ok := resp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		t.Fatal("should have at least one choice")
	}
}

func TestAPI_ChatCompletions_MissingModel(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	body := `{"messages": [{"role": "user", "content": "Hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPI_ChatCompletions_Streaming(t *testing.T) {
	mgr, db := newTestMgr(t)
	defer db.Close()
	setupModel(t, mgr, "test-model")

	backend := engine.NewMockBackend()
	pool := engine.NewPool(backend, 1024*1024*1024, mgr.Resolve)
	defer pool.UnloadAll()

	srv := NewServer(pool, mgr)

	body := `{
		"model": "test-model",
		"messages": [{"role": "user", "content": "Hello"}],
		"stream": true
	}`

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Check SSE format
	respBody := w.Body.String()
	if !strings.Contains(respBody, "data: ") {
		t.Error("streaming response should contain 'data: ' prefix")
	}
	if !strings.Contains(respBody, "[DONE]") {
		t.Error("streaming response should end with [DONE]")
	}
}

// ─── OpenAI /v1/embeddings ──────────────────────────────────────────────────

func TestAPI_Embeddings(t *testing.T) {
	mgr, db := newTestMgr(t)
	defer db.Close()
	setupModel(t, mgr, "test-model")

	backend := engine.NewMockBackend()
	pool := engine.NewPool(backend, 1024*1024*1024, mgr.Resolve)
	defer pool.UnloadAll()

	srv := NewServer(pool, mgr)

	body := `{"model": "test-model", "input": "hello"}`
	req := httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["object"] != "list" {
		t.Errorf("object = %q, want \"list\"", resp["object"])
	}
}

// ─── Ollama /api/tags ───────────────────────────────────────────────────────

func TestAPI_OllamaTags(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/tags", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)

	if _, ok := body["models"]; !ok {
		t.Error("response should contain 'models' key")
	}
}

// ─── Ollama /api/ps ─────────────────────────────────────────────────────────

func TestAPI_OllamaPs(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/ps", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// ─── Ollama /api/pull ───────────────────────────────────────────────────────

func TestAPI_OllamaPull(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	body := `{"name": "llama3"}`
	req := httptest.NewRequest("POST", "/api/pull", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// ─── CORS ───────────────────────────────────────────────────────────────────

func TestAPI_CORS(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("OPTIONS", "/v1/models", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("CORS: Access-Control-Allow-Origin should be *")
	}
}

// ─── BuildPrompt ────────────────────────────────────────────────────────────

func TestBuildPrompt(t *testing.T) {
	messages := []chatMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	}

	prompt := buildPrompt(messages)

	if !strings.Contains(prompt, "[SYSTEM] You are helpful.") {
		t.Error("prompt should contain system message")
	}
	if !strings.Contains(prompt, "[USER] Hello") {
		t.Error("prompt should contain user message")
	}
	if !strings.Contains(prompt, "[ASSISTANT] ") {
		t.Error("prompt should end with assistant marker")
	}
}

// Ensure unused import of os is used
var _ = os.TempDir
var _ = io.Discard
