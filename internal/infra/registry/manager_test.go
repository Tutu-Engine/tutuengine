package registry

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/sqlite"
)

// newTestManager creates a Manager backed by a local HTTP test server.
// Tests never hit the real network — all downloads serve fake GGUF data.
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()

	dbDir := filepath.Join(dir, "db")
	db, err := sqlite.Open(dbDir)
	if err != nil {
		t.Fatalf("Open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Local HTTP server serving fake GGUF content
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := []byte("GGUF-FAKE-MODEL-DATA-FOR-TESTING-" + r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	t.Cleanup(srv.Close)

	modelsDir := filepath.Join(dir, "models")
	mgr := NewManager(modelsDir, db)
	mgr.urlOverride = srv.URL
	return mgr
}

// ─── ParseRef Tests ─────────────────────────────────────────────────────────

func TestParseRef(t *testing.T) {
	tests := []struct {
		input   string
		name    string
		tag     string
	}{
		{"llama3", "llama3", "latest"},
		{"llama3:7b", "llama3", "7b"},
		{"mymodel:v2.1", "mymodel", "v2.1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ref := ParseRef(tt.input)
			if ref.Name != tt.name {
				t.Errorf("Name = %q, want %q", ref.Name, tt.name)
			}
			if ref.Tag != tt.tag {
				t.Errorf("Tag = %q, want %q", ref.Tag, tt.tag)
			}
		})
	}
}

// ─── Init Tests ─────────────────────────────────────────────────────────────

func TestManager_Init(t *testing.T) {
	mgr := newTestManager(t)

	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Check directories exist
	blobsDir := filepath.Join(mgr.dir, "blobs")
	manifestsDir := filepath.Join(mgr.dir, "manifests")

	if _, err := os.Stat(blobsDir); os.IsNotExist(err) {
		t.Error("blobs directory should exist")
	}
	if _, err := os.Stat(manifestsDir); os.IsNotExist(err) {
		t.Error("manifests directory should exist")
	}
}

// ─── Pull Tests ─────────────────────────────────────────────────────────────

func TestManager_Pull(t *testing.T) {
	mgr := newTestManager(t)

	var lastStatus string
	var lastPct float64
	err := mgr.Pull("llama3", func(status string, pct float64) {
		lastStatus = status
		lastPct = pct
	})
	if err != nil {
		t.Fatalf("Pull() error: %v", err)
	}

	if lastStatus != "done" {
		t.Errorf("lastStatus = %q, want \"done\"", lastStatus)
	}
	if lastPct != 100 {
		t.Errorf("lastPct = %f, want 100", lastPct)
	}
}

func TestManager_Pull_AlreadyExists(t *testing.T) {
	mgr := newTestManager(t)

	// Pull once
	if err := mgr.Pull("llama3", nil); err != nil {
		t.Fatalf("first Pull() error: %v", err)
	}

	// Pull again — should be a no-op
	var gotStatus string
	err := mgr.Pull("llama3", func(status string, pct float64) {
		gotStatus = status
	})
	if err != nil {
		t.Fatalf("second Pull() error: %v", err)
	}
	if gotStatus != "already exists" {
		t.Errorf("status = %q, want \"already exists\"", gotStatus)
	}
}

// ─── HasLocal Tests ─────────────────────────────────────────────────────────

func TestManager_HasLocal(t *testing.T) {
	mgr := newTestManager(t)

	// Before pull
	exists, err := mgr.HasLocal(ParseRef("llama3"))
	if err != nil {
		t.Fatalf("HasLocal() error: %v", err)
	}
	if exists {
		t.Error("model should not exist before pull")
	}

	// After pull
	if err := mgr.Pull("llama3", nil); err != nil {
		t.Fatalf("Pull() error: %v", err)
	}

	exists, err = mgr.HasLocal(ParseRef("llama3"))
	if err != nil {
		t.Fatalf("HasLocal() error: %v", err)
	}
	if !exists {
		t.Error("model should exist after pull")
	}
}

// ─── Resolve Tests ──────────────────────────────────────────────────────────

func TestManager_Resolve(t *testing.T) {
	mgr := newTestManager(t)

	if err := mgr.Pull("llama3", nil); err != nil {
		t.Fatalf("Pull() error: %v", err)
	}

	path, err := mgr.Resolve("llama3")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("resolved path %q does not exist", path)
	}
}

func TestManager_Resolve_NotFound(t *testing.T) {
	mgr := newTestManager(t)

	_, err := mgr.Resolve("nonexistent")
	if err != domain.ErrModelNotFound {
		t.Errorf("Resolve(nonexistent) = %v, want ErrModelNotFound", err)
	}
}

// ─── List Tests ─────────────────────────────────────────────────────────────

func TestManager_List(t *testing.T) {
	mgr := newTestManager(t)

	// Empty
	models, err := mgr.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(models) != 0 && models != nil {
		t.Errorf("expected empty list, got %d", len(models))
	}

	// After pulls
	for _, name := range []string{"llama3", "mistral", "phi3"} {
		if err := mgr.Pull(name, nil); err != nil {
			t.Fatalf("Pull(%s) error: %v", name, err)
		}
	}

	models, err = mgr.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(models) != 3 {
		t.Errorf("len(models) = %d, want 3", len(models))
	}
}

// ─── Show Tests ─────────────────────────────────────────────────────────────

func TestManager_Show(t *testing.T) {
	mgr := newTestManager(t)

	if err := mgr.Pull("llama3", nil); err != nil {
		t.Fatalf("Pull() error: %v", err)
	}

	info, err := mgr.Show("llama3")
	if err != nil {
		t.Fatalf("Show() error: %v", err)
	}

	if info.Name != "llama3" {
		t.Errorf("Name = %q, want %q", info.Name, "llama3")
	}
	if info.Format != "gguf" {
		t.Errorf("Format = %q, want %q", info.Format, "gguf")
	}
}

func TestManager_Show_NotFound(t *testing.T) {
	mgr := newTestManager(t)

	_, err := mgr.Show("ghost")
	if err != domain.ErrModelNotFound {
		t.Errorf("Show(ghost) = %v, want ErrModelNotFound", err)
	}
}

// ─── Remove Tests ───────────────────────────────────────────────────────────

func TestManager_Remove(t *testing.T) {
	mgr := newTestManager(t)

	if err := mgr.Pull("llama3", nil); err != nil {
		t.Fatalf("Pull() error: %v", err)
	}

	if err := mgr.Remove("llama3"); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	exists, err := mgr.HasLocal(ParseRef("llama3"))
	if err != nil {
		t.Fatalf("HasLocal() error: %v", err)
	}
	if exists {
		t.Error("model should not exist after Remove()")
	}
}

// ─── CreateFromTuTufile Tests ───────────────────────────────────────────────

func TestManager_CreateFromTuTufile(t *testing.T) {
	mgr := newTestManager(t)

	tf := domain.TuTufile{
		From:   "llama3",
		System: "You are a pirate.",
		Parameters: map[string][]string{
			"temperature": {"0.8"},
		},
	}

	if err := mgr.CreateFromTuTufile("my-pirate", tf); err != nil {
		t.Fatalf("CreateFromTuTufile() error: %v", err)
	}

	// Verify it's in the list
	info, err := mgr.Show("my-pirate")
	if err != nil {
		t.Fatalf("Show() error: %v", err)
	}
	if info.Name != "my-pirate" {
		t.Errorf("Name = %q, want %q", info.Name, "my-pirate")
	}
}

// ─── BlobPath / ManifestPath Tests ──────────────────────────────────────────

func TestManager_BlobPath(t *testing.T) {
	mgr := NewManager("/root/models", nil)
	got := mgr.BlobPath("sha256:abc123")
	want := filepath.Join("/root/models", "blobs", "sha256-abc123")
	if got != want {
		t.Errorf("BlobPath() = %q, want %q", got, want)
	}
}

func TestManager_ManifestPath(t *testing.T) {
	mgr := NewManager("/root/models", nil)
	ref := domain.ModelRef{Name: "llama3", Tag: "7b"}
	got := mgr.ManifestPath(ref)
	want := filepath.Join("/root/models", "manifests", "llama3", "7b")
	if got != want {
		t.Errorf("ManifestPath() = %q, want %q", got, want)
	}
}

func TestManager_ManifestPath_DefaultTag(t *testing.T) {
	mgr := NewManager("/root/models", nil)
	ref := domain.ModelRef{Name: "llama3"}
	got := mgr.ManifestPath(ref)
	want := filepath.Join("/root/models", "manifests", "llama3", "latest")
	if got != want {
		t.Errorf("ManifestPath() = %q, want %q", got, want)
	}
}
