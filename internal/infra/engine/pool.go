// Package engine provides the inference engine abstraction and model pool.
// The actual llama.cpp CGO backend is behind the InferenceBackend interface,
// allowing clean testing with mock implementations.
package engine

import (
	"context"
	"container/list"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── InferenceBackend Interface ─────────────────────────────────────────────
// This abstracts the actual llama.cpp CGO layer.
// Phase 0 ships with CGO backend; tests use MockBackend.

// InferenceBackend is the low-level model loading/inference interface.
type InferenceBackend interface {
	LoadModel(path string, opts LoadOptions) (ModelHandle, error)
	Close()
}

// ModelHandle represents a loaded model in memory.
type ModelHandle interface {
	Generate(ctx context.Context, prompt string, params GenerateParams) (<-chan domain.Token, error)
	Embed(ctx context.Context, input []string) ([][]float32, error)
	MemoryBytes() uint64
	Close()
}

// LoadOptions configures model loading.
type LoadOptions struct {
	NumGPULayers int // -1 = auto, 0 = CPU only, N = specific
	NumCtx       int // Context window size (default 4096)
	NumThreads   int // 0 = auto (runtime.NumCPU())
}

// GenerateParams holds sampling parameters.
type GenerateParams struct {
	Temperature float32
	TopP        float32
	MaxTokens   int
	Stop        []string
}

// ─── Model Pool (LRU + Reference Counting) ──────────────────────────────────
// Architecture Part V: Hash map + doubly-linked list.
// All operations O(1). Zero-leak via defer handle.Release().

// Pool manages loaded models with LRU eviction and reference counting.
type Pool struct {
	mu       sync.Mutex
	models   map[string]*poolEntry
	lru      *list.List
	maxMem   uint64
	usedMem  uint64
	backend  InferenceBackend
	resolver func(name string) (string, error) // name → file path
	idleTimeout  time.Duration
	reapInterval time.Duration
}

type poolEntry struct {
	handle   ModelHandle
	name     string
	memBytes uint64
	refCount int32
	element  *list.Element
	lastUsed time.Time
}

// PoolHandle is returned by Acquire. Caller MUST call Release() (use defer).
type PoolHandle struct {
	entry *poolEntry
	pool  *Pool
}

// NewPool creates a model pool with bounded memory.
func NewPool(backend InferenceBackend, maxMemBytes uint64, resolver func(string) (string, error)) *Pool {
	return &Pool{
		models:      make(map[string]*poolEntry),
		lru:         list.New(),
		maxMem:      maxMemBytes,
		backend:     backend,
		resolver:    resolver,
		idleTimeout:  5 * time.Minute,
		reapInterval: 30 * time.Second,
	}
}

// Acquire loads or retrieves a cached model. Returns a handle with ref count.
// Caller MUST call handle.Release() when done (use defer).
func (p *Pool) Acquire(name string, opts LoadOptions) (*PoolHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Cache hit — O(1)
	if entry, ok := p.models[name]; ok {
		atomic.AddInt32(&entry.refCount, 1)
		entry.lastUsed = time.Now()
		p.lru.MoveToFront(entry.element)
		return &PoolHandle{entry: entry, pool: p}, nil
	}

	// Resolve name → file path
	path, err := p.resolver(name)
	if err != nil {
		return nil, fmt.Errorf("resolve model %q: %w", name, err)
	}

	// Load model
	handle, err := p.backend.LoadModel(path, opts)
	if err != nil {
		return nil, fmt.Errorf("load model %q: %w", name, err)
	}

	memNeeded := handle.MemoryBytes()

	// Evict LRU models if needed to fit
	for p.usedMem+memNeeded > p.maxMem && p.lru.Len() > 0 {
		if !p.evictOne() {
			handle.Close()
			return nil, domain.ErrPoolExhausted
		}
	}

	entry := &poolEntry{
		handle:   handle,
		name:     name,
		memBytes: memNeeded,
		refCount: 1,
		lastUsed: time.Now(),
	}
	entry.element = p.lru.PushFront(entry)
	p.models[name] = entry
	p.usedMem += memNeeded

	return &PoolHandle{entry: entry, pool: p}, nil
}

// evictOne removes the least-recently-used model with refCount == 0.
func (p *Pool) evictOne() bool {
	for e := p.lru.Back(); e != nil; e = e.Prev() {
		entry := e.Value.(*poolEntry)
		if atomic.LoadInt32(&entry.refCount) == 0 {
			entry.handle.Close()
			p.lru.Remove(e)
			delete(p.models, entry.name)
			p.usedMem -= entry.memBytes
			return true
		}
	}
	return false
}

// Model returns the underlying model handle.
func (h *PoolHandle) Model() ModelHandle { return h.entry.handle }

// Release decrements the reference count. Must be called when done.
func (h *PoolHandle) Release() {
	atomic.AddInt32(&h.entry.refCount, -1)
}

// LoadedModels returns info about all models currently in the pool.
func (p *Pool) LoadedModels() []domain.LoadedModel {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := make([]domain.LoadedModel, 0, len(p.models))
	for name, entry := range p.models {
		processor := "CPU"
		result = append(result, domain.LoadedModel{
			Name:      name,
			SizeBytes: int64(entry.memBytes),
			Processor: processor,
			ExpiresAt: entry.lastUsed.Add(p.idleTimeout),
		})
	}
	return result
}

// UnloadAll releases all models from the pool.
func (p *Pool) UnloadAll() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for name, entry := range p.models {
		entry.handle.Close()
		p.lru.Remove(entry.element)
		delete(p.models, name)
	}
	p.usedMem = 0
	return nil
}

// IdleReaper runs in background, unloading models idle > timeout.
func (p *Pool) IdleReaper(ctx context.Context) {
	ticker := time.NewTicker(p.reapInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.mu.Lock()
			now := time.Now()
			for name, entry := range p.models {
				if now.Sub(entry.lastUsed) > p.idleTimeout && atomic.LoadInt32(&entry.refCount) == 0 {
					entry.handle.Close()
					p.lru.Remove(entry.element)
					delete(p.models, name)
					p.usedMem -= entry.memBytes
				}
			}
			p.mu.Unlock()
		}
	}
}
