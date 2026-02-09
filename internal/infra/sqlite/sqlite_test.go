package sqlite

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ─── Database Lifecycle ─────────────────────────────────────────────────────

func TestOpen_CreatesDatabase(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()

	// Check file exists
	if _, err := os.Stat(filepath.Join(dir, "state.db")); os.IsNotExist(err) {
		t.Error("state.db should exist")
	}
}

func TestOpen_Ping(t *testing.T) {
	db := newTestDB(t)
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

// ─── Model CRUD ─────────────────────────────────────────────────────────────

func TestUpsertModel_Insert(t *testing.T) {
	db := newTestDB(t)

	info := domain.ModelInfo{
		Name:      "llama3:latest",
		Digest:    "sha256:abc123",
		SizeBytes: 4_000_000_000,
		Format:    "gguf",
		PulledAt:  time.Now(),
	}

	if err := db.UpsertModel(info); err != nil {
		t.Fatalf("UpsertModel() error: %v", err)
	}

	got, err := db.GetModel("llama3:latest")
	if err != nil {
		t.Fatalf("GetModel() error: %v", err)
	}
	if got == nil {
		t.Fatal("GetModel() returned nil")
	}
	if got.Name != "llama3:latest" {
		t.Errorf("Name = %q, want %q", got.Name, "llama3:latest")
	}
	if got.SizeBytes != 4_000_000_000 {
		t.Errorf("SizeBytes = %d, want 4000000000", got.SizeBytes)
	}
}

func TestUpsertModel_Update(t *testing.T) {
	db := newTestDB(t)

	info := domain.ModelInfo{
		Name:      "llama3:latest",
		Digest:    "sha256:old",
		SizeBytes: 1000,
		Format:    "gguf",
		PulledAt:  time.Now(),
	}
	if err := db.UpsertModel(info); err != nil {
		t.Fatalf("first UpsertModel() error: %v", err)
	}

	// Update
	info.Digest = "sha256:new"
	info.SizeBytes = 2000
	if err := db.UpsertModel(info); err != nil {
		t.Fatalf("second UpsertModel() error: %v", err)
	}

	got, err := db.GetModel("llama3:latest")
	if err != nil {
		t.Fatalf("GetModel() error: %v", err)
	}
	if got.Digest != "sha256:new" {
		t.Errorf("Digest = %q, want %q", got.Digest, "sha256:new")
	}
	if got.SizeBytes != 2000 {
		t.Errorf("SizeBytes = %d, want 2000", got.SizeBytes)
	}
}

func TestGetModel_NotFound(t *testing.T) {
	db := newTestDB(t)

	got, err := db.GetModel("nonexistent")
	if err != nil {
		t.Fatalf("GetModel() error: %v", err)
	}
	if got != nil {
		t.Error("GetModel() should return nil for nonexistent model")
	}
}

func TestListModels(t *testing.T) {
	db := newTestDB(t)

	// Insert multiple models
	for _, name := range []string{"llama3", "mistral", "phi3"} {
		if err := db.UpsertModel(domain.ModelInfo{
			Name:      name,
			Digest:    "sha256:" + name,
			SizeBytes: 100,
			Format:    "gguf",
			PulledAt:  time.Now(),
		}); err != nil {
			t.Fatalf("UpsertModel(%s) error: %v", name, err)
		}
	}

	models, err := db.ListModels()
	if err != nil {
		t.Fatalf("ListModels() error: %v", err)
	}

	if len(models) != 3 {
		t.Errorf("len(models) = %d, want 3", len(models))
	}
}

func TestListModels_Empty(t *testing.T) {
	db := newTestDB(t)

	models, err := db.ListModels()
	if err != nil {
		t.Fatalf("ListModels() error: %v", err)
	}

	if models == nil {
		// nil is acceptable for empty result
	} else if len(models) != 0 {
		t.Errorf("len(models) = %d, want 0", len(models))
	}
}

func TestDeleteModel(t *testing.T) {
	db := newTestDB(t)

	if err := db.UpsertModel(domain.ModelInfo{
		Name:      "to-delete",
		Digest:    "sha256:del",
		SizeBytes: 100,
		Format:    "gguf",
		PulledAt:  time.Now(),
	}); err != nil {
		t.Fatalf("UpsertModel() error: %v", err)
	}

	if err := db.DeleteModel("to-delete"); err != nil {
		t.Fatalf("DeleteModel() error: %v", err)
	}

	got, err := db.GetModel("to-delete")
	if err != nil {
		t.Fatalf("GetModel() error: %v", err)
	}
	if got != nil {
		t.Error("model should be deleted")
	}
}

func TestDeleteModel_NotFound(t *testing.T) {
	db := newTestDB(t)

	err := db.DeleteModel("ghost")
	if err != domain.ErrModelNotFound {
		t.Errorf("DeleteModel(ghost) = %v, want ErrModelNotFound", err)
	}
}

func TestTouchModel(t *testing.T) {
	db := newTestDB(t)

	if err := db.UpsertModel(domain.ModelInfo{
		Name:      "touchme",
		Digest:    "sha256:touch",
		SizeBytes: 100,
		Format:    "gguf",
		PulledAt:  time.Now().Add(-24 * time.Hour),
	}); err != nil {
		t.Fatalf("UpsertModel() error: %v", err)
	}

	if err := db.TouchModel("touchme"); err != nil {
		t.Fatalf("TouchModel() error: %v", err)
	}

	got, err := db.GetModel("touchme")
	if err != nil {
		t.Fatalf("GetModel() error: %v", err)
	}

	// LastUsed should be roughly now
	if time.Since(got.LastUsed) > 5*time.Second {
		t.Errorf("LastUsed too old: %v", got.LastUsed)
	}
}

// ─── Node Info ──────────────────────────────────────────────────────────────

func TestNodeInfo_SetAndGet(t *testing.T) {
	db := newTestDB(t)

	if err := db.SetNodeInfo("node_id", "abc123"); err != nil {
		t.Fatalf("SetNodeInfo() error: %v", err)
	}

	got, err := db.GetNodeInfo("node_id")
	if err != nil {
		t.Fatalf("GetNodeInfo() error: %v", err)
	}
	if got != "abc123" {
		t.Errorf("GetNodeInfo() = %q, want %q", got, "abc123")
	}
}

func TestNodeInfo_Upsert(t *testing.T) {
	db := newTestDB(t)

	if err := db.SetNodeInfo("key", "v1"); err != nil {
		t.Fatalf("first SetNodeInfo() error: %v", err)
	}
	if err := db.SetNodeInfo("key", "v2"); err != nil {
		t.Fatalf("second SetNodeInfo() error: %v", err)
	}

	got, err := db.GetNodeInfo("key")
	if err != nil {
		t.Fatalf("GetNodeInfo() error: %v", err)
	}
	if got != "v2" {
		t.Errorf("GetNodeInfo() = %q, want %q", got, "v2")
	}
}

func TestNodeInfo_NotFound(t *testing.T) {
	db := newTestDB(t)

	got, err := db.GetNodeInfo("missing")
	if err != nil {
		t.Fatalf("GetNodeInfo() error: %v", err)
	}
	if got != "" {
		t.Errorf("GetNodeInfo(missing) = %q, want empty", got)
	}
}
