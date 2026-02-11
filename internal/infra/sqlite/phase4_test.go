package sqlite

import (
	"path/filepath"
	"testing"
)

// ─── Phase 4 Migration Tests ────────────────────────────────────────────────

func TestPhase4Migrations_TablesCreated(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Verify all Phase 4 tables exist by inserting and querying
	tables := []string{
		"finetune_jobs",
		"finetune_shards",
		"gradient_updates",
		"finetune_checkpoints",
		"marketplace_listings",
		"marketplace_benchmarks",
		"marketplace_reviews",
		"quality_checks",
		"chunk_transfers",
	}
	for _, table := range tables {
		var count int
		err := db.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
		if err != nil {
			t.Errorf("table %s not accessible: %v", table, err)
		}
	}
}

// ─── Fine-Tuning CRUD Tests ────────────────────────────────────────────────

func TestPhase4_FineTuneJob_Lifecycle(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	// Insert job
	err := db.InsertFineTuneJob("ft-1", "llama3.2", "s3://data/train.jsonl", "lora", 3, 2, 10, `{"rank":16}`)
	if err != nil {
		t.Fatalf("InsertFineTuneJob: %v", err)
	}

	// Read back
	baseModel, datasetURI, method, status, configJSON, epochs, creditCost, err := db.GetFineTuneJob("ft-1")
	if err != nil {
		t.Fatalf("GetFineTuneJob: %v", err)
	}
	if baseModel != "llama3.2" {
		t.Errorf("baseModel = %q, want llama3.2", baseModel)
	}
	if datasetURI != "s3://data/train.jsonl" {
		t.Errorf("datasetURI = %q", datasetURI)
	}
	if method != "lora" {
		t.Errorf("method = %q, want lora", method)
	}
	if status != "PENDING" {
		t.Errorf("status = %q, want PENDING", status)
	}
	if configJSON != `{"rank":16}` {
		t.Errorf("config = %q", configJSON)
	}
	if epochs != 3 {
		t.Errorf("epochs = %d, want 3", epochs)
	}
	if creditCost != 0 {
		t.Errorf("creditCost = %d, want 0", creditCost)
	}

	// Update status to TRAINING
	if err := db.UpdateFineTuneJobStatus("ft-1", "TRAINING", nil); err != nil {
		t.Fatalf("UpdateStatus TRAINING: %v", err)
	}

	_, _, _, status, _, _, _, _ = db.GetFineTuneJob("ft-1")
	if status != "TRAINING" {
		t.Errorf("after update: status = %q, want TRAINING", status)
	}

	// Add credits
	if err := db.AddFineTuneCredits("ft-1", 50); err != nil {
		t.Fatalf("AddFineTuneCredits: %v", err)
	}
	_, _, _, _, _, _, creditCost, _ = db.GetFineTuneJob("ft-1")
	if creditCost != 50 {
		t.Errorf("creditCost = %d, want 50", creditCost)
	}

	// Complete with error
	errMsg := "OOM"
	if err := db.UpdateFineTuneJobStatus("ft-1", "FAILED", &errMsg); err != nil {
		t.Fatalf("UpdateStatus FAILED: %v", err)
	}
}

func TestPhase4_ListFineTuneJobs(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.InsertFineTuneJob("j1", "model-a", "uri", "lora", 3, 2, 10, "{}")
	db.InsertFineTuneJob("j2", "model-b", "uri", "qlora", 5, 3, 8, "{}")

	jobs, err := db.ListFineTuneJobs(10)
	if err != nil {
		t.Fatalf("ListFineTuneJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("got %d jobs, want 2", len(jobs))
	}
}

// ─── Gradient Update Tests ──────────────────────────────────────────────────

func TestPhase4_GradientUpdates(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.InsertFineTuneJob("gj", "base", "uri", "lora", 3, 1, 5, "{}")
	if err := db.InsertGradientUpdate("gj", "node-1", 0, 1, 2.5, 100); err != nil {
		t.Fatalf("InsertGradientUpdate: %v", err)
	}
	if err := db.InsertGradientUpdate("gj", "node-2", 1, 1, 1.8, 200); err != nil {
		t.Fatalf("InsertGradientUpdate 2: %v", err)
	}

	cnt, err := db.CountEpochGradients("gj", 1)
	if err != nil {
		t.Fatalf("CountEpochGradients: %v", err)
	}
	if cnt != 2 {
		t.Errorf("epoch 1 gradients = %d, want 2", cnt)
	}

	// Epoch 2 should have none
	cnt, _ = db.CountEpochGradients("gj", 2)
	if cnt != 0 {
		t.Errorf("epoch 2 gradients = %d, want 0", cnt)
	}
}

// ─── Marketplace Listing Tests ──────────────────────────────────────────────

func TestPhase4_MarketplaceListing_CRUD(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	err := db.InsertMarketplaceListing(
		"ml-1", "CodeHelper", "llama3", "alice",
		"A code assistant", "code", `["code","python"]`,
		"1.0", 1024*1024*100, "sha256-abc", 50,
	)
	if err != nil {
		t.Fatalf("InsertMarketplaceListing: %v", err)
	}

	// Approve
	if err := db.UpdateListingStatus("ml-1", "APPROVED"); err != nil {
		t.Fatalf("UpdateListingStatus: %v", err)
	}

	// Search
	results, err := db.SearchListings("code", "", 10)
	if err != nil {
		t.Fatalf("SearchListings: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("search results = %d, want 1", len(results))
	}
	if results[0].ModelName != "CodeHelper" {
		t.Errorf("model name = %q", results[0].ModelName)
	}

	// Download tracking
	if err := db.RecordListingDownload("ml-1", 40); err != nil {
		t.Fatalf("RecordListingDownload: %v", err)
	}
	downloads, err := db.GetListingDownloads("ml-1")
	if err != nil {
		t.Fatalf("GetListingDownloads: %v", err)
	}
	if downloads != 1 {
		t.Errorf("downloads = %d, want 1", downloads)
	}
}

func TestPhase4_SearchListings_CategoryFilter(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.InsertMarketplaceListing("a", "ChatModel", "base", "x", "chat", "chat", "[]", "1", 100, "d", 10)
	db.InsertMarketplaceListing("b", "CodeModel", "base", "y", "code", "code", "[]", "1", 100, "d", 20)
	db.UpdateListingStatus("a", "APPROVED")
	db.UpdateListingStatus("b", "APPROVED")

	results, _ := db.SearchListings("chat", "", 10)
	if len(results) != 1 || results[0].ID != "a" {
		t.Errorf("chat filter results = %v", results)
	}
}

func TestPhase4_SearchListings_TextQuery(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.InsertMarketplaceListing("q1", "Python Helper", "base", "x", "helps with python", "code", "[]", "1", 100, "d", 10)
	db.UpdateListingStatus("q1", "APPROVED")

	results, _ := db.SearchListings("", "python", 10)
	if len(results) != 1 {
		t.Errorf("text query results = %d, want 1", len(results))
	}
}

// ─── Review Tests ───────────────────────────────────────────────────────────

func TestPhase4_Reviews(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.InsertMarketplaceListing("rev", "Model", "base", "alice", "", "general", "[]", "1", 100, "d", 10)

	if err := db.InsertReview("r1", "rev", "bob", 5, "Great!"); err != nil {
		t.Fatalf("InsertReview: %v", err)
	}
	if err := db.InsertReview("r2", "rev", "carol", 3, "OK"); err != nil {
		t.Fatalf("InsertReview 2: %v", err)
	}

	avg, cnt, err := db.AverageListingRating("rev")
	if err != nil {
		t.Fatalf("AverageListingRating: %v", err)
	}
	if cnt != 2 {
		t.Errorf("review count = %d, want 2", cnt)
	}
	if avg != 4.0 {
		t.Errorf("average = %f, want 4.0", avg)
	}
}

func TestPhase4_ReviewUniqueConstraint(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.InsertMarketplaceListing("uc", "Model", "base", "alice", "", "general", "[]", "1", 100, "d", 10)
	db.InsertReview("r1", "uc", "bob", 5, "First")

	// Same author, same listing → should fail (UNIQUE constraint)
	err := db.InsertReview("r2", "uc", "bob", 1, "Second")
	if err == nil {
		t.Error("duplicate review by same author should fail")
	}
}

// ─── Quality Check Tests ────────────────────────────────────────────────────

func TestPhase4_QualityCheck(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.InsertMarketplaceListing("qc", "Model", "base", "alice", "", "general", "[]", "1", 100, "d", 10)

	err := db.UpsertQualityCheck("qc", true, true, true, true, "[]")
	if err != nil {
		t.Fatalf("UpsertQualityCheck: %v", err)
	}

	// Upsert again (update path)
	err = db.UpsertQualityCheck("qc", false, true, false, false, `["issue"]`)
	if err != nil {
		t.Fatalf("UpsertQualityCheck update: %v", err)
	}
}

// ─── Chunk Transfer Tests ───────────────────────────────────────────────────

func TestPhase4_ChunkTransfer(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if err := db.InsertChunkTransfer("manifest-1", 0, "peer-A", "peer-B", 4*1024*1024); err != nil {
		t.Fatalf("InsertChunkTransfer: %v", err)
	}
	if err := db.InsertChunkTransfer("manifest-1", 1, "peer-A", "peer-B", 4*1024*1024); err != nil {
		t.Fatalf("InsertChunkTransfer 2: %v", err)
	}

	// Check progress: 0 of 2 completed
	total, completed, err := db.TransferProgress("manifest-1", "peer-B")
	if err != nil {
		t.Fatalf("TransferProgress: %v", err)
	}
	if total != 2 || completed != 0 {
		t.Errorf("progress = %d/%d, want 0/2", completed, total)
	}

	// Complete chunk 0
	if err := db.CompleteChunkTransfer("manifest-1", 0, "peer-B"); err != nil {
		t.Fatalf("CompleteChunkTransfer: %v", err)
	}

	_, completed, _ = db.TransferProgress("manifest-1", "peer-B")
	if completed != 1 {
		t.Errorf("completed = %d, want 1", completed)
	}
}

// ─── Helper ─────────────────────────────────────────────────────────────────

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "test-phase4")
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open test DB: %v", err)
	}
	return db
}
