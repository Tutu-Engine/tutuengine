package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tutu-network/tutu/internal/infra/sqlite"
)

func newTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(dir)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ─── Checker Tests ──────────────────────────────────────────────────────────

func TestNewChecker(t *testing.T) {
	db := newTestDB(t)
	modelsDir := t.TempDir()

	c := NewChecker(db, modelsDir)
	if c == nil {
		t.Fatal("NewChecker() returned nil")
	}
	if len(c.checks) != 3 {
		t.Errorf("checks = %d, want 3", len(c.checks))
	}
}

func TestChecker_RunAllHealthy(t *testing.T) {
	db := newTestDB(t)
	modelsDir := t.TempDir()

	c := NewChecker(db, modelsDir)
	ctx := context.Background()
	c.runAll(ctx)

	statuses := c.Statuses()
	if len(statuses) != 3 {
		t.Fatalf("Statuses() = %d, want 3", len(statuses))
	}

	for _, s := range statuses {
		if !s.Healthy {
			t.Errorf("check %q should be healthy, got error: %s", s.Name, s.Error)
		}
	}
}

func TestChecker_IsHealthy_AllPass(t *testing.T) {
	db := newTestDB(t)
	modelsDir := t.TempDir()

	c := NewChecker(db, modelsDir)
	c.runAll(context.Background())

	if !c.IsHealthy() {
		t.Error("IsHealthy() should be true when all checks pass")
	}
}

func TestChecker_IsHealthy_BeforeRun(t *testing.T) {
	db := newTestDB(t)
	modelsDir := t.TempDir()

	c := NewChecker(db, modelsDir)

	// Before any run, there are no statuses — IsHealthy returns true (vacuously)
	if !c.IsHealthy() {
		t.Error("IsHealthy() should be true before first run (no statuses)")
	}
}

func TestChecker_SQLiteCheck(t *testing.T) {
	db := newTestDB(t)
	modelsDir := t.TempDir()

	c := NewChecker(db, modelsDir)
	c.runAll(context.Background())

	statuses := c.Statuses()
	found := false
	for _, s := range statuses {
		if s.Name == "sqlite" {
			found = true
			if !s.Healthy {
				t.Errorf("sqlite check should be healthy")
			}
		}
	}
	if !found {
		t.Error("sqlite check not found in statuses")
	}
}

func TestChecker_DiskSpaceCheck(t *testing.T) {
	db := newTestDB(t)
	modelsDir := t.TempDir()

	c := NewChecker(db, modelsDir)
	c.runAll(context.Background())

	statuses := c.Statuses()
	for _, s := range statuses {
		if s.Name == "disk_space" {
			if !s.Healthy {
				t.Errorf("disk_space check should be healthy")
			}
		}
	}
}

func TestChecker_ModelIntegrityCheck_NoDir(t *testing.T) {
	db := newTestDB(t)
	// Use non-existent dir — should be fine (no models yet)
	modelsDir := filepath.Join(t.TempDir(), "nonexistent")

	c := NewChecker(db, modelsDir)
	c.runAll(context.Background())

	if !c.IsHealthy() {
		statuses := c.Statuses()
		for _, s := range statuses {
			if !s.Healthy {
				t.Errorf("check %q failed: %s", s.Name, s.Error)
			}
		}
	}
}

func TestChecker_ModelIntegrityCheck_FileNotDir(t *testing.T) {
	db := newTestDB(t)
	// Create a file where models dir should be
	modelsDir := filepath.Join(t.TempDir(), "models")
	os.WriteFile(modelsDir, []byte("not a dir"), 0644)

	c := NewChecker(db, modelsDir)
	c.runAll(context.Background())

	// model_integrity check should fail
	statuses := c.Statuses()
	for _, s := range statuses {
		if s.Name == "model_integrity" {
			if s.Healthy {
				t.Error("model_integrity should fail when path is a file")
			}
		}
	}
}

func TestChecker_CustomCheck(t *testing.T) {
	c := &Checker{
		checks: []Check{
			{
				Name: "always_pass",
				CheckFn: func(ctx context.Context) error {
					return nil
				},
			},
		},
	}

	c.runAll(context.Background())

	statuses := c.Statuses()
	if len(statuses) != 1 {
		t.Fatalf("statuses = %d, want 1", len(statuses))
	}
	if !statuses[0].Healthy {
		t.Error("always_pass check should be healthy")
	}
}

func TestChecker_FailingCheck(t *testing.T) {
	c := &Checker{
		checks: []Check{
			{
				Name: "always_fail",
				CheckFn: func(ctx context.Context) error {
					return os.ErrPermission
				},
			},
		},
	}

	c.runAll(context.Background())

	statuses := c.Statuses()
	if statuses[0].Healthy {
		t.Error("always_fail check should not be healthy")
	}
	if statuses[0].Error == "" {
		t.Error("error message should be populated")
	}
}

func TestChecker_StatusesCopy(t *testing.T) {
	db := newTestDB(t)
	c := NewChecker(db, t.TempDir())
	c.runAll(context.Background())

	s1 := c.Statuses()
	s2 := c.Statuses()

	// Verify it's a copy, not the same slice
	if len(s1) > 0 {
		s1[0].Healthy = false
		if !s2[0].Healthy {
			t.Error("Statuses() should return a copy, not a reference")
		}
	}
}
