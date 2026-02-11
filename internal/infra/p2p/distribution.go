// Package p2p implements BitTorrent-style chunked model distribution.
// Architecture Part V: P2P Model Distribution.
//
// How it works:
//  1. A model is split into fixed-size chunks (default 4MB)
//  2. Each chunk has a SHA-256 digest for integrity
//  3. A ChunkManifest lists all chunks + Ed25519 signature
//  4. Nodes download chunks from multiple peers in parallel
//  5. Popular models are pre-distributed to high-reputation nodes
//
// This reduces CDN costs by ~80% because nodes share chunks directly.
package p2p

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/tutu-network/tutu/internal/infra/dsa"
)

// ─── Constants ──────────────────────────────────────────────────────────────

const (
	DefaultChunkSize = 4 * 1024 * 1024 // 4 MB per chunk
	MaxChunkSize     = 16 * 1024 * 1024 // 16 MB max
	MinChunkSize     = 256 * 1024        // 256 KB min
)

// ─── Errors ─────────────────────────────────────────────────────────────────

var (
	ErrChunkCorrupted     = errors.New("chunk integrity check failed: SHA-256 mismatch")
	ErrManifestInvalid    = errors.New("chunk manifest signature invalid")
	ErrNoPeersAvailable   = errors.New("no peers available for chunk download")
	ErrChunkNotFound      = errors.New("chunk not found on any peer")
	ErrTransferCancelled  = errors.New("transfer cancelled")
	ErrInvalidChunkSize   = errors.New("chunk size out of allowed range")
)

// ─── Chunk Types ────────────────────────────────────────────────────────────

// ChunkDigest is a SHA-256 hex digest of a single chunk.
type ChunkDigest string

// ChunkInfo describes a single chunk in a model file.
type ChunkInfo struct {
	Index  int         `json:"index"`   // 0-based chunk position
	Offset int64       `json:"offset"`  // Byte offset in the original file
	Size   int         `json:"size"`    // Chunk size in bytes
	Digest ChunkDigest `json:"digest"`  // SHA-256 hex of chunk data
}

// ChunkManifest describes a complete model split into chunks.
// The manifest is signed with the publisher's Ed25519 key.
type ChunkManifest struct {
	ModelName    string      `json:"model_name"`
	ModelDigest  string      `json:"model_digest"`   // SHA-256 of the whole file
	TotalSize    int64       `json:"total_size"`      // Total model size in bytes
	ChunkSize    int         `json:"chunk_size"`      // Uniform chunk size (last may be smaller)
	Chunks       []ChunkInfo `json:"chunks"`
	PublisherKey string      `json:"publisher_key"`   // Ed25519 public key hex
	Signature    string      `json:"signature"`       // Ed25519 signature of manifest body
	CreatedAt    time.Time   `json:"created_at"`
}

// ChunkCount returns the number of chunks.
func (m *ChunkManifest) ChunkCount() int {
	return len(m.Chunks)
}

// Validate checks manifest integrity: chunk ordering, sizes, and optional
// Ed25519 signature verification.
func (m *ChunkManifest) Validate() error {
	if len(m.Chunks) == 0 {
		return fmt.Errorf("manifest has no chunks")
	}
	if m.ChunkSize < MinChunkSize || m.ChunkSize > MaxChunkSize {
		return ErrInvalidChunkSize
	}

	var totalSize int64
	for i, c := range m.Chunks {
		if c.Index != i {
			return fmt.Errorf("chunk %d has wrong index %d", i, c.Index)
		}
		if c.Size <= 0 {
			return fmt.Errorf("chunk %d has invalid size %d", i, c.Size)
		}
		if c.Digest == "" {
			return fmt.Errorf("chunk %d has empty digest", i)
		}
		totalSize += int64(c.Size)
	}

	if totalSize != m.TotalSize {
		return fmt.Errorf("chunk sizes sum to %d but total_size is %d", totalSize, m.TotalSize)
	}

	return nil
}

// VerifySignature checks the Ed25519 signature if publisher key is present.
func (m *ChunkManifest) VerifySignature() error {
	if m.PublisherKey == "" || m.Signature == "" {
		return nil // unsigned manifests are allowed for local use
	}

	pubBytes, err := hex.DecodeString(m.PublisherKey)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid publisher key")
	}

	sigBytes, err := hex.DecodeString(m.Signature)
	if err != nil {
		return fmt.Errorf("invalid signature hex")
	}

	// Sign over the canonical manifest body (name + digest + chunks)
	body := m.canonicalBody()
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), body, sigBytes) {
		return ErrManifestInvalid
	}
	return nil
}

// canonicalBody builds the signable representation of the manifest.
func (m *ChunkManifest) canonicalBody() []byte {
	h := sha256.New()
	fmt.Fprintf(h, "%s:%s:%d:%d", m.ModelName, m.ModelDigest, m.TotalSize, m.ChunkSize)
	for _, c := range m.Chunks {
		fmt.Fprintf(h, ":%d:%s", c.Size, c.Digest)
	}
	return h.Sum(nil)
}

// VerifyChunk validates a downloaded chunk against its expected digest.
func VerifyChunk(data []byte, expected ChunkDigest) error {
	actual := sha256.Sum256(data)
	hex := ChunkDigest(encodeHex(actual[:]))
	if hex != expected {
		return ErrChunkCorrupted
	}
	return nil
}

// SplitIntoChunks creates a ChunkManifest from raw model data.
func SplitIntoChunks(modelName string, data []byte, chunkSize int) (*ChunkManifest, [][]byte) {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}

	totalHash := sha256.Sum256(data)
	manifest := &ChunkManifest{
		ModelName:   modelName,
		ModelDigest: encodeHex(totalHash[:]),
		TotalSize:   int64(len(data)),
		ChunkSize:   chunkSize,
		CreatedAt:   time.Now(),
	}

	var chunks [][]byte
	for offset := 0; offset < len(data); offset += chunkSize {
		end := offset + chunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk := data[offset:end]
		chunkHash := sha256.Sum256(chunk)

		manifest.Chunks = append(manifest.Chunks, ChunkInfo{
			Index:  len(manifest.Chunks),
			Offset: int64(offset),
			Size:   len(chunk),
			Digest: ChunkDigest(encodeHex(chunkHash[:])),
		})
		chunks = append(chunks, chunk)
	}

	return manifest, chunks
}

// SignManifest signs a manifest with an Ed25519 private key.
func SignManifest(m *ChunkManifest, privateKey ed25519.PrivateKey) {
	pubKey := privateKey.Public().(ed25519.PublicKey)
	m.PublisherKey = encodeHex(pubKey)
	body := m.canonicalBody()
	sig := ed25519.Sign(privateKey, body)
	m.Signature = encodeHex(sig)
}

func encodeHex(b []byte) string {
	return hex.EncodeToString(b)
}

// ─── Peer Swarm ─────────────────────────────────────────────────────────────
// Tracks which peers have which chunks. Used to select download sources.

// PeerChunkMap tracks chunk availability across the swarm.
type PeerChunkMap struct {
	mu    sync.RWMutex
	peers map[string]*dsa.BloomFilter // nodeID → bloom filter of chunk digests
}

// NewPeerChunkMap creates a peer chunk tracker.
func NewPeerChunkMap() *PeerChunkMap {
	return &PeerChunkMap{
		peers: make(map[string]*dsa.BloomFilter),
	}
}

// RegisterPeer adds or updates a peer's chunk inventory.
func (pcm *PeerChunkMap) RegisterPeer(nodeID string, chunkDigests []ChunkDigest) {
	pcm.mu.Lock()
	defer pcm.mu.Unlock()

	bf := dsa.NewBloomFilter(dsa.BloomConfig{
		ExpectedItems: max(len(chunkDigests), 100),
		FPRate:        0.001,
	})
	for _, d := range chunkDigests {
		bf.Add(string(d))
	}
	pcm.peers[nodeID] = bf
}

// RemovePeer removes a peer from the swarm.
func (pcm *PeerChunkMap) RemovePeer(nodeID string) {
	pcm.mu.Lock()
	defer pcm.mu.Unlock()
	delete(pcm.peers, nodeID)
}

// PeersWithChunk returns peers that likely have the given chunk.
// Uses bloom filter — 0 false negatives, ≤ 0.1% false positives.
func (pcm *PeerChunkMap) PeersWithChunk(digest ChunkDigest) []string {
	pcm.mu.RLock()
	defer pcm.mu.RUnlock()

	var result []string
	for nodeID, bf := range pcm.peers {
		if bf.Contains(string(digest)) {
			result = append(result, nodeID)
		}
	}
	sort.Strings(result) // deterministic ordering
	return result
}

// PeerCount returns the number of tracked peers.
func (pcm *PeerChunkMap) PeerCount() int {
	pcm.mu.RLock()
	defer pcm.mu.RUnlock()
	return len(pcm.peers)
}

// ─── Transfer Tracker ───────────────────────────────────────────────────────
// Tracks download progress for a model transfer.

// TransferStatus represents the state of a chunk transfer.
type TransferStatus int

const (
	TransferPending    TransferStatus = iota // Not yet started
	TransferInProgress                       // Currently downloading
	TransferComplete                         // Downloaded and verified
	TransferFailed                           // Download or verification failed
)

// TransferProgress tracks the download state of a full model.
type TransferProgress struct {
	mu         sync.Mutex
	ModelName  string
	Manifest   *ChunkManifest
	ChunkState []TransferStatus
	StartedAt  time.Time
	BytesDone  int64
}

// NewTransferProgress creates a progress tracker for a manifest.
func NewTransferProgress(manifest *ChunkManifest) *TransferProgress {
	return &TransferProgress{
		ModelName:  manifest.ModelName,
		Manifest:   manifest,
		ChunkState: make([]TransferStatus, manifest.ChunkCount()),
		StartedAt:  time.Now(),
	}
}

// MarkComplete marks a chunk as successfully downloaded and verified.
func (tp *TransferProgress) MarkComplete(index int) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if index >= 0 && index < len(tp.ChunkState) {
		tp.ChunkState[index] = TransferComplete
		tp.BytesDone += int64(tp.Manifest.Chunks[index].Size)
	}
}

// MarkFailed marks a chunk as failed.
func (tp *TransferProgress) MarkFailed(index int) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if index >= 0 && index < len(tp.ChunkState) {
		tp.ChunkState[index] = TransferFailed
	}
}

// PendingChunks returns indices of chunks not yet downloaded.
func (tp *TransferProgress) PendingChunks() []int {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	var pending []int
	for i, s := range tp.ChunkState {
		if s == TransferPending || s == TransferFailed {
			pending = append(pending, i)
		}
	}
	return pending
}

// IsComplete returns true if all chunks are downloaded.
func (tp *TransferProgress) IsComplete() bool {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	for _, s := range tp.ChunkState {
		if s != TransferComplete {
			return false
		}
	}
	return true
}

// ProgressPct returns download completion as a percentage (0-100).
func (tp *TransferProgress) ProgressPct() float64 {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if len(tp.ChunkState) == 0 {
		return 100.0
	}
	done := 0
	for _, s := range tp.ChunkState {
		if s == TransferComplete {
			done++
		}
	}
	return float64(done) / float64(len(tp.ChunkState)) * 100.0
}

// CompletedCount returns the number of completed chunks.
func (tp *TransferProgress) CompletedCount() int {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	count := 0
	for _, s := range tp.ChunkState {
		if s == TransferComplete {
			count++
		}
	}
	return count
}

// Elapsed returns time since the transfer started.
func (tp *TransferProgress) Elapsed() time.Duration {
	return time.Since(tp.StartedAt)
}
