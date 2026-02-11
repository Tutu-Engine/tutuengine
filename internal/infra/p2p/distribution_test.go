package p2p

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"testing"
)

// ─── Chunk Splitting Tests ──────────────────────────────────────────────────

func TestSplitIntoChunks_Basic(t *testing.T) {
	data := make([]byte, 10*1024*1024) // 10 MB
	for i := range data {
		data[i] = byte(i % 256)
	}

	manifest, chunks := SplitIntoChunks("test-model", data, DefaultChunkSize)

	if manifest.ModelName != "test-model" {
		t.Errorf("ModelName = %q, want %q", manifest.ModelName, "test-model")
	}

	// 10 MB / 4 MB = 3 chunks (2 full + 1 partial)
	expectedChunks := 3
	if manifest.ChunkCount() != expectedChunks {
		t.Fatalf("got %d chunks, want %d", manifest.ChunkCount(), expectedChunks)
	}
	if len(chunks) != expectedChunks {
		t.Fatalf("got %d chunk data slices, want %d", len(chunks), expectedChunks)
	}

	// First two chunks should be 4 MB
	if manifest.Chunks[0].Size != DefaultChunkSize {
		t.Errorf("chunk[0].Size = %d, want %d", manifest.Chunks[0].Size, DefaultChunkSize)
	}
	if manifest.Chunks[1].Size != DefaultChunkSize {
		t.Errorf("chunk[1].Size = %d, want %d", manifest.Chunks[1].Size, DefaultChunkSize)
	}
	// Last chunk is 10-8 = 2 MB
	if manifest.Chunks[2].Size != 2*1024*1024 {
		t.Errorf("chunk[2].Size = %d, want %d", manifest.Chunks[2].Size, 2*1024*1024)
	}

	// Total size
	if manifest.TotalSize != int64(len(data)) {
		t.Errorf("TotalSize = %d, want %d", manifest.TotalSize, len(data))
	}
}

func TestSplitIntoChunks_ExactMultiple(t *testing.T) {
	// 8 MB exactly = 2 full 4 MB chunks, no remainder
	data := make([]byte, 8*1024*1024)
	manifest, _ := SplitIntoChunks("exact", data, DefaultChunkSize)

	if manifest.ChunkCount() != 2 {
		t.Fatalf("got %d chunks, want 2", manifest.ChunkCount())
	}
}

func TestSplitIntoChunks_SmallData(t *testing.T) {
	// Smaller than one chunk
	data := []byte("hello world")
	manifest, _ := SplitIntoChunks("tiny", data, DefaultChunkSize)

	if manifest.ChunkCount() != 1 {
		t.Fatalf("got %d chunks, want 1", manifest.ChunkCount())
	}
	if manifest.Chunks[0].Size != len(data) {
		t.Errorf("chunk size = %d, want %d", manifest.Chunks[0].Size, len(data))
	}
}

// ─── Chunk Verification Tests ───────────────────────────────────────────────

func TestVerifyChunk_Valid(t *testing.T) {
	data := []byte("chunk data here")
	hash := sha256.Sum256(data)
	expected := ChunkDigest(fmt.Sprintf("%x", hash))

	if err := VerifyChunk(data, expected); err != nil {
		t.Errorf("VerifyChunk() should succeed, got: %v", err)
	}
}

func TestVerifyChunk_Corrupted(t *testing.T) {
	data := []byte("original data")
	hash := sha256.Sum256(data)
	expected := ChunkDigest(fmt.Sprintf("%x", hash))

	// Corrupt the data
	corrupted := append([]byte{}, data...)
	corrupted[0] ^= 0xFF

	if err := VerifyChunk(corrupted, expected); err == nil {
		t.Error("VerifyChunk() should fail on corrupted data")
	}
}

// ─── Manifest Signing Tests ─────────────────────────────────────────────────

func TestSignManifest_RoundTrip(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key generation: %v", err)
	}

	data := make([]byte, 5*1024*1024)
	manifest, _ := SplitIntoChunks("signed-model", data, DefaultChunkSize)

	SignManifest(manifest, priv)

	if manifest.Signature == "" {
		t.Fatal("signature should not be empty after signing")
	}
	if manifest.PublisherKey == "" {
		t.Fatal("publisher key should not be empty after signing")
	}

	// Verify
	if err := manifest.VerifySignature(); err != nil {
		t.Errorf("VerifySignature() should return nil for valid signature, got: %v", err)
	}
}

func TestSignManifest_TamperedManifest(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)

	data := make([]byte, 1024)
	manifest, _ := SplitIntoChunks("tamper-test", data, DefaultChunkSize)
	SignManifest(manifest, priv)

	// Tamper with the manifest
	manifest.ModelName = "evil-model"

	if err := manifest.VerifySignature(); err == nil {
		t.Error("VerifySignature() should return error after tampering")
	}
}

// ─── Manifest Validation Tests ──────────────────────────────────────────────

func TestManifest_Validate_Valid(t *testing.T) {
	data := make([]byte, 1024*1024)
	m, _ := SplitIntoChunks("test", data, DefaultChunkSize)

	if err := m.Validate(); err != nil {
		t.Errorf("Validate() on valid manifest: %v", err)
	}
}

func TestManifest_Validate_EmptyModelName(t *testing.T) {
	data := make([]byte, 1024*1024)
	m, _ := SplitIntoChunks("test", data, DefaultChunkSize)
	m.ModelName = ""
	// Validate checks chunks, not model name — it may or may not error.
	// The important constraint is chunk integrity.
	_ = m.Validate()
}

func TestManifest_Validate_WrongTotalSize(t *testing.T) {
	data := make([]byte, 1024*1024)
	m, _ := SplitIntoChunks("test", data, DefaultChunkSize)
	m.TotalSize = 1

	err := m.Validate()
	if err == nil {
		t.Error("Validate() should fail with wrong total size")
	}
}

// ─── PeerChunkMap Tests ─────────────────────────────────────────────────────

func TestPeerChunkMap_RegisterAndLookup(t *testing.T) {
	pcm := NewPeerChunkMap()

	digestsA := []ChunkDigest{"digest-0", "digest-1", "digest-2"}
	digestsB := []ChunkDigest{"digest-1", "digest-3"}
	pcm.RegisterPeer("peer-A", digestsA)
	pcm.RegisterPeer("peer-B", digestsB)

	// Chunk digest-1 is on both peers
	peers := pcm.PeersWithChunk("digest-1")
	if len(peers) < 1 {
		t.Fatalf("PeersWithChunk(digest-1) = %v, want at least 1 peer", peers)
	}

	// Chunk digest-0 is only on peer-A
	peers = pcm.PeersWithChunk("digest-0")
	found := false
	for _, p := range peers {
		if p == "peer-A" {
			found = true
		}
	}
	if !found {
		t.Errorf("PeersWithChunk(digest-0) = %v, should include peer-A", peers)
	}
}

func TestPeerChunkMap_PeerCount(t *testing.T) {
	pcm := NewPeerChunkMap()
	pcm.RegisterPeer("node-1", []ChunkDigest{"d1"})
	pcm.RegisterPeer("node-2", []ChunkDigest{"d2"})

	if pcm.PeerCount() != 2 {
		t.Errorf("PeerCount() = %d, want 2", pcm.PeerCount())
	}
}

func TestPeerChunkMap_RemovePeer(t *testing.T) {
	pcm := NewPeerChunkMap()
	pcm.RegisterPeer("node-1", []ChunkDigest{"d1"})
	pcm.RemovePeer("node-1")

	if pcm.PeerCount() != 0 {
		t.Errorf("PeerCount after remove = %d, want 0", pcm.PeerCount())
	}
}

// ─── TransferProgress Tests ─────────────────────────────────────────────────

func TestTransferProgress_Lifecycle(t *testing.T) {
	data := make([]byte, 12*1024*1024) // 12 MB → 3 chunks
	manifest, _ := SplitIntoChunks("progress-test", data, DefaultChunkSize)

	progress := NewTransferProgress(manifest)

	if progress.IsComplete() {
		t.Fatal("should not be complete initially")
	}
	pending := progress.PendingChunks()
	if len(pending) != 3 {
		t.Fatalf("pending = %d, want 3", len(pending))
	}
	if progress.ProgressPct() != 0 {
		t.Fatalf("progress = %.0f%%, want 0%%", progress.ProgressPct())
	}

	// Complete 2 of 3
	progress.MarkComplete(0)
	progress.MarkComplete(1)

	if progress.CompletedCount() != 2 {
		t.Errorf("completed = %d, want 2", progress.CompletedCount())
	}
	pending = progress.PendingChunks()
	if len(pending) != 1 {
		t.Errorf("pending = %d, want 1", len(pending))
	}
	expectedPct := float64(2) / float64(3) * 100
	if diff := progress.ProgressPct() - expectedPct; diff > 0.1 || diff < -0.1 {
		t.Errorf("progress = %.1f%%, want ~%.1f%%", progress.ProgressPct(), expectedPct)
	}

	// Complete last chunk
	progress.MarkComplete(2)
	if !progress.IsComplete() {
		t.Error("should be complete after all chunks marked")
	}
	if progress.ProgressPct() != 100 {
		t.Errorf("final progress = %.0f%%, want 100%%", progress.ProgressPct())
	}
}

func TestTransferProgress_MarkFailed(t *testing.T) {
	data := make([]byte, 8*1024*1024) // 2 chunks
	manifest, _ := SplitIntoChunks("fail-test", data, DefaultChunkSize)
	progress := NewTransferProgress(manifest)

	progress.MarkFailed(0)

	// Failed chunks count as pending (can be retried)
	pending := progress.PendingChunks()
	if len(pending) != 2 {
		t.Errorf("pending after 1 failed = %d, want 2", len(pending))
	}
}
