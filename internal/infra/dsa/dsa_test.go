package dsa

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// ─── Hash Ring Tests ────────────────────────────────────────────────────────

func TestHashRing_EmptyRing(t *testing.T) {
	t.Helper()
	ring := NewHashRing(DefaultHashRingConfig())

	if got := ring.Size(); got != 0 {
		t.Fatalf("empty ring size = %d, want 0", got)
	}
	if got := ring.Lookup("anything"); got != "" {
		t.Fatalf("empty ring lookup = %q, want empty", got)
	}
	if got := ring.LookupN("anything", 3); len(got) != 0 {
		t.Fatalf("empty ring LookupN = %v, want empty", got)
	}
}

func TestHashRing_SingleNode(t *testing.T) {
	ring := NewHashRing(DefaultHashRingConfig())
	ring.AddNode("node-1")

	if ring.Size() != 1 {
		t.Fatalf("size = %d, want 1", ring.Size())
	}

	// Every key must land on the only node
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("model-%d", i)
		if got := ring.Lookup(key); got != "node-1" {
			t.Errorf("Lookup(%q) = %q, want node-1", key, got)
		}
	}
}

func TestHashRing_Distribution(t *testing.T) {
	// With 3 nodes, each should get roughly 1/3 of keys.
	// Consistent hashing with 150 vnodes per node gives good balance.
	ring := NewHashRing(DefaultHashRingConfig())
	ring.AddNode("node-a")
	ring.AddNode("node-b")
	ring.AddNode("node-c")

	counts := map[string]int{}
	total := 10000
	for i := 0; i < total; i++ {
		node := ring.Lookup(fmt.Sprintf("key-%d", i))
		counts[node]++
	}

	// Each node should have between 20% and 46% of keys (generous bounds)
	for node, count := range counts {
		pct := float64(count) / float64(total)
		if pct < 0.20 || pct > 0.46 {
			t.Errorf("node %s got %.1f%% of keys (count=%d), expected ~33%%", node, pct*100, count)
		}
	}
}

func TestHashRing_AddRemoveConsistency(t *testing.T) {
	// After removing a node, keys that were on OTHER nodes should stay the same
	ring := NewHashRing(DefaultHashRingConfig())
	ring.AddNode("A")
	ring.AddNode("B")
	ring.AddNode("C")

	// Record where keys land with 3 nodes
	before := map[string]string{}
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("k-%d", i)
		before[key] = ring.Lookup(key)
	}

	// Remove node B
	ring.RemoveNode("B")

	// Keys that were on A or C should still be on A or C (not moved)
	moved := 0
	for key, oldNode := range before {
		if oldNode == "B" {
			continue // These must move
		}
		newNode := ring.Lookup(key)
		if newNode != oldNode {
			moved++
		}
	}

	// O(K/n) rebalancing: very few should move. Allow 5% tolerance.
	if float64(moved)/1000.0 > 0.05 {
		t.Errorf("too many keys moved after removing one node: %d/1000", moved)
	}
}

func TestHashRing_LookupN(t *testing.T) {
	ring := NewHashRing(DefaultHashRingConfig())
	ring.AddNode("x")
	ring.AddNode("y")
	ring.AddNode("z")

	nodes := ring.LookupN("test-key", 2)
	if len(nodes) != 2 {
		t.Fatalf("LookupN(2) returned %d nodes, want 2", len(nodes))
	}

	// Must be distinct
	if nodes[0] == nodes[1] {
		t.Errorf("LookupN returned duplicate: %v", nodes)
	}

	// LookupN(5) should return all 3 (capped)
	all := ring.LookupN("test-key", 5)
	if len(all) != 3 {
		t.Errorf("LookupN(5) returned %d, want 3 (total nodes)", len(all))
	}
}

func TestHashRing_Nodes(t *testing.T) {
	ring := NewHashRing(DefaultHashRingConfig())
	ring.AddNode("b")
	ring.AddNode("a")
	ring.AddNode("c")

	nodes := ring.Nodes()
	if len(nodes) != 3 {
		t.Fatalf("Nodes() = %d, want 3", len(nodes))
	}
	// Should be sorted
	for i := 1; i < len(nodes); i++ {
		if nodes[i] < nodes[i-1] {
			t.Fatalf("Nodes() not sorted: %v", nodes)
		}
	}
}

func TestHashRing_DuplicateAdd(t *testing.T) {
	ring := NewHashRing(DefaultHashRingConfig())
	ring.AddNode("node-1")
	ring.AddNode("node-1") // dup

	if ring.Size() != 1 {
		t.Fatalf("duplicate add: size = %d, want 1", ring.Size())
	}
}

// ─── Bloom Filter Tests ─────────────────────────────────────────────────────

func TestBloomFilter_AddContains(t *testing.T) {
	bf := NewBloomFilter(DefaultBloomConfig())

	items := []string{"alpha", "bravo", "charlie", "delta"}
	for _, item := range items {
		bf.Add(item)
	}

	for _, item := range items {
		if !bf.Contains(item) {
			t.Errorf("Contains(%q) = false after Add", item)
		}
	}

	if bf.Count() != len(items) {
		t.Errorf("Count() = %d, want %d", bf.Count(), len(items))
	}
}

func TestBloomFilter_FalsePositiveRate(t *testing.T) {
	cfg := BloomConfig{
		ExpectedItems: 10000,
		FPRate:        0.01, // 1%
	}
	bf := NewBloomFilter(cfg)

	// Insert 10000 items
	for i := 0; i < 10000; i++ {
		bf.Add(fmt.Sprintf("item-%d", i))
	}

	// Check 10000 items that were NOT inserted
	falsePositives := 0
	for i := 10000; i < 20000; i++ {
		if bf.Contains(fmt.Sprintf("item-%d", i)) {
			falsePositives++
		}
	}

	// Should be close to 1% (allow up to 2%)
	fpRate := float64(falsePositives) / 10000.0
	if fpRate > 0.02 {
		t.Errorf("false positive rate = %.2f%%, want < 2%%", fpRate*100)
	}

	// Estimated FP rate should be in the right ballpark
	estimated := bf.EstimatedFPRate()
	if estimated > 0.05 {
		t.Errorf("estimated FP rate = %.4f, want < 0.05", estimated)
	}
}

func TestBloomFilter_Reset(t *testing.T) {
	bf := NewBloomFilter(DefaultBloomConfig())
	bf.Add("test")

	if !bf.Contains("test") {
		t.Fatal("should contain 'test' before reset")
	}

	bf.Reset()

	if bf.Contains("test") {
		t.Error("should not contain 'test' after reset")
	}
	if bf.Count() != 0 {
		t.Errorf("count after reset = %d, want 0", bf.Count())
	}
}

func TestBloomFilter_Config(t *testing.T) {
	cfg := BloomConfig{ExpectedItems: 1000, FPRate: 0.01}
	bf := NewBloomFilter(cfg)

	numBits, numHash := bf.Config()
	if numBits == 0 || numHash == 0 {
		t.Fatalf("Config() returned zeros: bits=%d, hash=%d", numBits, numHash)
	}

	// Optimal formula: m = -(n * ln(p)) / (ln(2)^2)
	expectedBits := uint(math.Ceil(-float64(1000) * math.Log(0.01) / (math.Log(2) * math.Log(2))))
	if numBits != expectedBits {
		t.Errorf("bits = %d, want %d", numBits, expectedBits)
	}
}

func TestBloomFilter_EmptyContains(t *testing.T) {
	bf := NewBloomFilter(DefaultBloomConfig())
	if bf.Contains("never-added") {
		t.Error("empty bloom filter should not contain anything")
	}
}

// ─── Priority Queue Tests ───────────────────────────────────────────────────

func TestPriorityQueue_Basic(t *testing.T) {
	pq := NewPriorityQueue(DefaultPriorityQueueConfig())

	pq.Push(HeapItem{Key: "low", Priority: 10, SubmittedAt: time.Now()})
	pq.Push(HeapItem{Key: "high", Priority: 1, SubmittedAt: time.Now()})
	pq.Push(HeapItem{Key: "mid", Priority: 5, SubmittedAt: time.Now()})

	if pq.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", pq.Len())
	}

	item, ok := pq.Pop()
	if !ok || item.Key != "high" {
		t.Fatalf("first Pop = %q (ok=%v), want 'high'", item.Key, ok)
	}

	item, ok = pq.Pop()
	if !ok || item.Key != "mid" {
		t.Fatalf("second Pop = %q, want 'mid'", item.Key)
	}

	item, ok = pq.Pop()
	if !ok || item.Key != "low" {
		t.Fatalf("third Pop = %q, want 'low'", item.Key)
	}

	_, ok = pq.Pop()
	if ok {
		t.Error("Pop on empty queue should return false")
	}
}

func TestPriorityQueue_Peek(t *testing.T) {
	pq := NewPriorityQueue(DefaultPriorityQueueConfig())

	_, ok := pq.Peek()
	if ok {
		t.Error("Peek on empty queue should return false")
	}

	pq.Push(HeapItem{Key: "a", Priority: 5, SubmittedAt: time.Now()})
	item, ok := pq.Peek()
	if !ok || item.Key != "a" {
		t.Fatalf("Peek = %q (ok=%v), want 'a'", item.Key, ok)
	}

	// Peek should not remove it
	if pq.Len() != 1 {
		t.Fatalf("Len after Peek = %d, want 1", pq.Len())
	}
}

func TestPriorityQueue_StarvationPrevention(t *testing.T) {
	// With BoostInterval=5s and MaxBoost=2, a task waiting 10+ seconds
	// gets priority boosted by 2 levels.
	cfg := PriorityQueueConfig{
		BoostInterval: 5 * time.Second,
		MaxBoost:      2,
	}
	pq := NewPriorityQueue(cfg)

	// Override clock
	now := time.Now()
	pq.now = func() time.Time { return now }

	// Old low-priority task submitted 15 seconds ago
	oldItem := HeapItem{Key: "old", Priority: 10, SubmittedAt: now.Add(-15 * time.Second)}
	// New high-priority task submitted just now
	newItem := HeapItem{Key: "new", Priority: 8, SubmittedAt: now}

	pq.Push(oldItem)
	pq.Push(newItem)

	// "old" has effective priority = 10 - min(15/5, 2) = 10 - 2 = 8
	// "new" has effective priority = 8 - min(0/5, 2) = 8 - 0 = 8
	// Same effective priority → FIFO → "old" wins (earlier SubmittedAt)
	item, _ := pq.Pop()
	if item.Key != "old" {
		t.Errorf("expected 'old' (starvation-boosted) to be dequeued first, got %q", item.Key)
	}
}

func TestPriorityQueue_FIFOTieBreaker(t *testing.T) {
	pq := NewPriorityQueue(DefaultPriorityQueueConfig())

	now := time.Now()
	pq.now = func() time.Time { return now }

	// Same priority, different submission times
	pq.Push(HeapItem{Key: "first", Priority: 5, SubmittedAt: now.Add(-2 * time.Second)})
	pq.Push(HeapItem{Key: "second", Priority: 5, SubmittedAt: now.Add(-1 * time.Second)})
	pq.Push(HeapItem{Key: "third", Priority: 5, SubmittedAt: now})

	// Should come out in submission order (FIFO)
	expected := []string{"first", "second", "third"}
	for _, want := range expected {
		item, ok := pq.Pop()
		if !ok || item.Key != want {
			t.Errorf("Pop = %q, want %q", item.Key, want)
		}
	}
}

func TestPriorityQueue_ConcurrentSafety(t *testing.T) {
	pq := NewPriorityQueue(DefaultPriorityQueueConfig())
	done := make(chan struct{})

	// Push from 10 goroutines
	for g := 0; g < 10; g++ {
		go func(id int) {
			for i := 0; i < 100; i++ {
				pq.Push(HeapItem{
					Key:         fmt.Sprintf("g%d-i%d", id, i),
					Priority:    i,
					SubmittedAt: time.Now(),
				})
			}
			done <- struct{}{}
		}(g)
	}

	for g := 0; g < 10; g++ {
		<-done
	}

	if pq.Len() != 1000 {
		t.Errorf("Len = %d after concurrent pushes, want 1000", pq.Len())
	}

	// Pop everything
	count := 0
	for {
		_, ok := pq.Pop()
		if !ok {
			break
		}
		count++
	}
	if count != 1000 {
		t.Errorf("popped %d items, want 1000", count)
	}
}
