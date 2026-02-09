package engine

import (
	"context"
	"sync"
	"testing"
	"time"
)

// ─── Mock Backend Tests ─────────────────────────────────────────────────────

func TestMockBackend_LoadModel(t *testing.T) {
	b := NewMockBackend()
	defer b.Close()

	h, err := b.LoadModel("test-model", LoadOptions{})
	if err != nil {
		t.Fatalf("LoadModel() error: %v", err)
	}
	defer h.Close()

	if h.MemoryBytes() == 0 {
		t.Error("MemoryBytes() should be non-zero")
	}
}

func TestMockBackend_Generate(t *testing.T) {
	b := NewMockBackend()
	defer b.Close()

	h, err := b.LoadModel("test-model", LoadOptions{})
	if err != nil {
		t.Fatalf("LoadModel() error: %v", err)
	}
	defer h.Close()

	ctx := context.Background()
	tokenCh, err := h.Generate(ctx, "Hello, world!", GenerateParams{MaxTokens: 5})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	var tokens []string
	for tok := range tokenCh {
		tokens = append(tokens, tok.Text)
	}

	if len(tokens) == 0 {
		t.Error("Generate() should produce at least one token")
	}
}

func TestMockBackend_Embed(t *testing.T) {
	b := NewMockBackend()
	defer b.Close()

	h, err := b.LoadModel("test-model", LoadOptions{})
	if err != nil {
		t.Fatalf("LoadModel() error: %v", err)
	}
	defer h.Close()

	ctx := context.Background()
	embeddings, err := h.Embed(ctx, []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}

	if len(embeddings) != 2 {
		t.Errorf("len(embeddings) = %d, want 2", len(embeddings))
	}

	for i, emb := range embeddings {
		if len(emb) == 0 {
			t.Errorf("embeddings[%d] is empty", i)
		}
	}
}

// ─── Pool Tests ─────────────────────────────────────────────────────────────

func newTestPool() *Pool {
	backend := NewMockBackend()
	resolver := func(name string) (string, error) {
		return "/fake/path/" + name, nil
	}
	return NewPool(backend, 1024*1024*1024, resolver) // 1GB limit
}

func TestPool_AcquireAndRelease(t *testing.T) {
	pool := newTestPool()

	h, err := pool.Acquire("test-model", LoadOptions{})
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	if h.Model() == nil {
		t.Error("Model() should not be nil")
	}

	h.Release()
}

func TestPool_CacheHit(t *testing.T) {
	pool := newTestPool()

	// First acquire
	h1, err := pool.Acquire("test-model", LoadOptions{})
	if err != nil {
		t.Fatalf("first Acquire() error: %v", err)
	}
	h1.Release()

	// Second acquire should be a cache hit
	h2, err := pool.Acquire("test-model", LoadOptions{})
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}
	h2.Release()

	// Should have same underlying model
	if h1.Model() != h2.Model() {
		t.Error("second Acquire() should return cached model")
	}
}

func TestPool_MultipleModels(t *testing.T) {
	pool := newTestPool()

	h1, err := pool.Acquire("model-a", LoadOptions{})
	if err != nil {
		t.Fatalf("Acquire(model-a) error: %v", err)
	}
	defer h1.Release()

	h2, err := pool.Acquire("model-b", LoadOptions{})
	if err != nil {
		t.Fatalf("Acquire(model-b) error: %v", err)
	}
	defer h2.Release()

	if h1.Model() == h2.Model() {
		t.Error("different models should have different handles")
	}
}

func TestPool_LoadedModels(t *testing.T) {
	pool := newTestPool()

	h, err := pool.Acquire("test-model", LoadOptions{})
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}
	defer h.Release()

	loaded := pool.LoadedModels()
	if len(loaded) != 1 {
		t.Fatalf("LoadedModels() = %d, want 1", len(loaded))
	}

	if loaded[0].Name != "test-model" {
		t.Errorf("Name = %q, want %q", loaded[0].Name, "test-model")
	}
}

func TestPool_UnloadAll(t *testing.T) {
	pool := newTestPool()

	h, err := pool.Acquire("test-model", LoadOptions{})
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}
	h.Release()

	if err := pool.UnloadAll(); err != nil {
		t.Fatalf("UnloadAll() error: %v", err)
	}

	loaded := pool.LoadedModels()
	if len(loaded) != 0 {
		t.Errorf("LoadedModels() after UnloadAll = %d, want 0", len(loaded))
	}
}

func TestPool_ConcurrentAcquire(t *testing.T) {
	pool := newTestPool()

	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := pool.Acquire("shared-model", LoadOptions{})
			if err != nil {
				errCh <- err
				return
			}
			defer h.Release()

			// Use the model
			ctx := context.Background()
			tokenCh, err := h.Model().Generate(ctx, "test", GenerateParams{MaxTokens: 1})
			if err != nil {
				errCh <- err
				return
			}
			for range tokenCh {
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}
}

func TestPool_IdleReaper(t *testing.T) {
	backend := NewMockBackend()
	resolver := func(name string) (string, error) {
		return "/fake/path/" + name, nil
	}
	pool := NewPool(backend, 1024*1024*1024, resolver)
	pool.idleTimeout = 50 * time.Millisecond   // Very short for testing
	pool.reapInterval = 25 * time.Millisecond   // Tick fast enough to catch it

	h, err := pool.Acquire("test-model", LoadOptions{})
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}
	h.Release()

	// Start reaper
	ctx, cancel := context.WithCancel(context.Background())
	go pool.IdleReaper(ctx)

	// Wait for reaper to run
	time.Sleep(200 * time.Millisecond)
	cancel()

	loaded := pool.LoadedModels()
	if len(loaded) != 0 {
		t.Errorf("model should have been reaped, got %d loaded", len(loaded))
	}
}

func TestPool_GenerateThroughHandle(t *testing.T) {
	pool := newTestPool()

	h, err := pool.Acquire("gen-test", LoadOptions{})
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}
	defer h.Release()

	ctx := context.Background()
	tokenCh, err := h.Model().Generate(ctx, "test prompt", GenerateParams{
		Temperature: 0.5,
		MaxTokens:   3,
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	count := 0
	for range tokenCh {
		count++
	}
	if count == 0 {
		t.Error("should generate at least one token")
	}
}
