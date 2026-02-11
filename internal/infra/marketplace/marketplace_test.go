package marketplace

import (
	"fmt"
	"testing"
)

// ─── Store Tests ────────────────────────────────────────────────────────────

func newTestStore() *Store {
	return NewStore(StoreConfig{
		MaxListingsPerCreator: 3,
		MinPrice:              1,
		MaxPrice:              1000,
		CreatorSharePct:       80,
	})
}

func TestStore_Publish(t *testing.T) {
	s := newTestStore()

	listing := Listing{
		ID:        "model-1",
		ModelName: "CodeLlama Fine-tuned",
		Creator:   "alice",
		Price:     50,
		SizeBytes: 1024,
		Digest:    "abc123",
	}
	if err := s.Publish(listing); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	got, err := s.GetListing("model-1")
	if err != nil {
		t.Fatalf("GetListing: %v", err)
	}
	if got.Status != StatusPending {
		t.Errorf("status = %s, want PENDING", got.Status)
	}
	if got.Downloads != 0 {
		t.Errorf("downloads = %d, want 0", got.Downloads)
	}
}

func TestStore_PublishDuplicate(t *testing.T) {
	s := newTestStore()

	s.Publish(Listing{ID: "dup", Creator: "bob", Price: 10, SizeBytes: 1, Digest: "x"})
	err := s.Publish(Listing{ID: "dup", Creator: "bob", Price: 10, SizeBytes: 1, Digest: "x"})
	if err != ErrAlreadyPublished {
		t.Errorf("err = %v, want ErrAlreadyPublished", err)
	}
}

func TestStore_MaxListingsPerCreator(t *testing.T) {
	s := newTestStore() // max 3

	for i := 0; i < 3; i++ {
		s.Publish(Listing{
			ID: fmt.Sprintf("m-%d", i), Creator: "alice",
			Price: 10, SizeBytes: 1, Digest: "d",
		})
	}

	err := s.Publish(Listing{ID: "m-4", Creator: "alice", Price: 10, SizeBytes: 1, Digest: "d"})
	if err == nil {
		t.Error("expected max listings error, got nil")
	}

	// Different creator should still work
	err = s.Publish(Listing{ID: "m-bob", Creator: "bob", Price: 10, SizeBytes: 1, Digest: "d"})
	if err != nil {
		t.Errorf("different creator publish: %v", err)
	}
}

func TestStore_PriceValidation(t *testing.T) {
	s := newTestStore() // min 1, max 1000

	err := s.Publish(Listing{ID: "too-cheap", Creator: "x", Price: 0, SizeBytes: 1, Digest: "d"})
	if err == nil {
		t.Error("price 0 should be rejected")
	}

	err = s.Publish(Listing{ID: "too-expensive", Creator: "x", Price: 9999, SizeBytes: 1, Digest: "d"})
	if err == nil {
		t.Error("price 9999 should be rejected")
	}
}

func TestStore_ListingNotFound(t *testing.T) {
	s := newTestStore()
	_, err := s.GetListing("nope")
	if err != ErrListingNotFound {
		t.Errorf("err = %v, want ErrListingNotFound", err)
	}
}

// ─── Quality Check & Approval Tests ─────────────────────────────────────────

func TestStore_ApproveQuality(t *testing.T) {
	s := newTestStore()
	s.Publish(Listing{ID: "qa", Creator: "alice", Price: 10, SizeBytes: 1, Digest: "d"})

	// Pass quality check
	err := s.ApproveQuality(QualityCheck{
		ListingID:   "qa",
		Passed:      true,
		Signatures:  true,
		NoMalware:   true,
		Benchmarked: true,
	})
	if err != nil {
		t.Fatalf("ApproveQuality: %v", err)
	}

	got, _ := s.GetListing("qa")
	if got.Status != StatusApproved {
		t.Errorf("status = %s, want APPROVED", got.Status)
	}
}

func TestStore_RejectQuality(t *testing.T) {
	s := newTestStore()
	s.Publish(Listing{ID: "bad", Creator: "bob", Price: 10, SizeBytes: 1, Digest: "d"})

	s.ApproveQuality(QualityCheck{
		ListingID: "bad",
		Passed:    false,
		Issues:    []string{"malware detected"},
	})

	got, _ := s.GetListing("bad")
	if got.Status != StatusRejected {
		t.Errorf("status = %s, want REJECTED", got.Status)
	}
}

// ─── Download & Revenue Tests ───────────────────────────────────────────────

func TestStore_RecordDownload(t *testing.T) {
	s := newTestStore() // 80% creator share
	s.Publish(Listing{ID: "dl", Creator: "alice", Price: 100, SizeBytes: 1, Digest: "d"})
	s.ApproveQuality(QualityCheck{ListingID: "dl", Passed: true})

	share, err := s.RecordDownload("dl")
	if err != nil {
		t.Fatalf("RecordDownload: %v", err)
	}
	// 80% of 100 = 80
	if share != 80 {
		t.Errorf("creator share = %d, want 80", share)
	}

	got, _ := s.GetListing("dl")
	if got.Downloads != 1 {
		t.Errorf("downloads = %d, want 1", got.Downloads)
	}
	if got.TotalRevenue != 80 {
		t.Errorf("revenue = %d, want 80", got.TotalRevenue)
	}
}

func TestStore_RecordDownload_Unapproved(t *testing.T) {
	s := newTestStore()
	s.Publish(Listing{ID: "unverified", Creator: "bob", Price: 10, SizeBytes: 1, Digest: "d"})

	_, err := s.RecordDownload("unverified")
	if err != ErrModelUnverified {
		t.Errorf("err = %v, want ErrModelUnverified", err)
	}
}

// ─── Search Tests ───────────────────────────────────────────────────────────

func setupSearchStore(t *testing.T) *Store {
	t.Helper()
	s := newTestStore()

	listings := []Listing{
		{ID: "1", ModelName: "CodeAssistant", Category: CatCode, Creator: "a", Price: 50, SizeBytes: 1, Digest: "d"},
		{ID: "2", ModelName: "ChatBot Plus", Category: CatChat, Creator: "b", Price: 30, SizeBytes: 1, Digest: "d"},
		{ID: "3", ModelName: "Science Helper", Category: CatScience, Creator: "a", Price: 20, SizeBytes: 1, Digest: "d"},
	}
	for _, l := range listings {
		s.Publish(l)
		s.ApproveQuality(QualityCheck{ListingID: l.ID, Passed: true})
	}

	// Give model "1" some downloads so it ranks higher
	s.RecordDownload("1")
	s.RecordDownload("1")
	s.RecordDownload("1")
	s.RecordDownload("2")

	return s
}

func TestStore_SearchByCategory(t *testing.T) {
	s := setupSearchStore(t)

	results := s.Search(CatCode, "")
	if len(results) != 1 {
		t.Fatalf("code results = %d, want 1", len(results))
	}
	if results[0].ID != "1" {
		t.Errorf("got ID = %s, want 1", results[0].ID)
	}
}

func TestStore_SearchByQuery(t *testing.T) {
	s := setupSearchStore(t)

	results := s.Search("", "chat")
	if len(results) != 1 {
		t.Fatalf("chat results = %d, want 1", len(results))
	}
	if results[0].ModelName != "ChatBot Plus" {
		t.Errorf("got %s, want ChatBot Plus", results[0].ModelName)
	}
}

func TestStore_SearchSortedByDownloads(t *testing.T) {
	s := setupSearchStore(t)

	results := s.Search("", "")
	if len(results) != 3 {
		t.Fatalf("all results = %d, want 3", len(results))
	}
	// Sorted by downloads desc: model 1 (3) > model 2 (1) > model 3 (0)
	if results[0].ID != "1" {
		t.Errorf("first result ID = %s, want 1 (most downloads)", results[0].ID)
	}
}

func TestStore_SearchCaseInsensitive(t *testing.T) {
	s := setupSearchStore(t)

	results := s.Search("", "SCIENCE")
	if len(results) != 1 {
		t.Fatalf("case-insensitive results = %d, want 1", len(results))
	}
}

func TestStore_SearchNoResults(t *testing.T) {
	s := setupSearchStore(t)

	results := s.Search("", "nonexistent-query")
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}

// ─── Review Tests ───────────────────────────────────────────────────────────

func TestStore_AddReview(t *testing.T) {
	s := newTestStore()
	s.Publish(Listing{ID: "rev", Creator: "alice", Price: 10, SizeBytes: 1, Digest: "d"})

	err := s.AddReview(Review{
		ID:        "r1",
		ListingID: "rev",
		Author:    "bob",
		Rating:    5,
		Comment:   "Amazing model!",
	})
	if err != nil {
		t.Fatalf("AddReview: %v", err)
	}

	reviews := s.Reviews("rev")
	if len(reviews) != 1 {
		t.Fatalf("reviews = %d, want 1", len(reviews))
	}
	if reviews[0].Rating != 5 {
		t.Errorf("rating = %d, want 5", reviews[0].Rating)
	}
}

func TestStore_SelfReview(t *testing.T) {
	s := newTestStore()
	s.Publish(Listing{ID: "self", Creator: "alice", Price: 10, SizeBytes: 1, Digest: "d"})

	err := s.AddReview(Review{
		ListingID: "self",
		Author:    "alice", // same as creator
		Rating:    5,
	})
	if err != ErrSelfReview {
		t.Errorf("err = %v, want ErrSelfReview", err)
	}
}

func TestStore_DuplicateReview(t *testing.T) {
	s := newTestStore()
	s.Publish(Listing{ID: "duprev", Creator: "alice", Price: 10, SizeBytes: 1, Digest: "d"})

	s.AddReview(Review{ID: "r1", ListingID: "duprev", Author: "bob", Rating: 4})
	err := s.AddReview(Review{ID: "r2", ListingID: "duprev", Author: "bob", Rating: 3})
	if err != ErrDuplicateReview {
		t.Errorf("err = %v, want ErrDuplicateReview", err)
	}
}

func TestStore_ReviewValidation(t *testing.T) {
	s := newTestStore()
	s.Publish(Listing{ID: "val", Creator: "alice", Price: 10, SizeBytes: 1, Digest: "d"})

	// Invalid rating
	err := s.AddReview(Review{ListingID: "val", Author: "bob", Rating: 0})
	if err == nil {
		t.Error("rating 0 should be rejected")
	}

	err = s.AddReview(Review{ListingID: "val", Author: "bob", Rating: 6})
	if err == nil {
		t.Error("rating 6 should be rejected")
	}
}

func TestStore_AverageRating(t *testing.T) {
	s := newTestStore()
	s.Publish(Listing{ID: "avg", Creator: "alice", Price: 10, SizeBytes: 1, Digest: "d"})

	s.AddReview(Review{ID: "r1", ListingID: "avg", Author: "bob", Rating: 4})
	s.AddReview(Review{ID: "r2", ListingID: "avg", Author: "carol", Rating: 2})

	avg := s.AverageRating("avg")
	if avg != 3.0 {
		t.Errorf("average = %f, want 3.0", avg)
	}
}

func TestStore_AverageRatingEmpty(t *testing.T) {
	s := newTestStore()
	avg := s.AverageRating("nonexistent")
	if avg != 0 {
		t.Errorf("empty average = %f, want 0", avg)
	}
}

// ─── Delist Tests ───────────────────────────────────────────────────────────

func TestStore_Delist(t *testing.T) {
	s := newTestStore()
	s.Publish(Listing{ID: "delist", Creator: "alice", Price: 10, SizeBytes: 1, Digest: "d"})

	if err := s.DelistListing("delist"); err != nil {
		t.Fatalf("DelistListing: %v", err)
	}

	got, _ := s.GetListing("delist")
	if got.Status != StatusDelisted {
		t.Errorf("status = %s, want DELISTED", got.Status)
	}
}

// ─── ListByCreator Tests ────────────────────────────────────────────────────

func TestStore_ListByCreator(t *testing.T) {
	s := newTestStore()
	s.Publish(Listing{ID: "a1", Creator: "alice", Price: 10, SizeBytes: 1, Digest: "d"})
	s.Publish(Listing{ID: "a2", Creator: "alice", Price: 20, SizeBytes: 1, Digest: "d"})
	s.Publish(Listing{ID: "b1", Creator: "bob", Price: 15, SizeBytes: 1, Digest: "d"})

	aliceModels := s.ListByCreator("alice")
	if len(aliceModels) != 2 {
		t.Errorf("alice models = %d, want 2", len(aliceModels))
	}
}

// ─── Stats Tests ────────────────────────────────────────────────────────────

func TestStore_Stats(t *testing.T) {
	s := newTestStore()
	s.Publish(Listing{ID: "s1", Creator: "alice", Price: 100, SizeBytes: 1, Digest: "d"})
	s.Publish(Listing{ID: "s2", Creator: "bob", Price: 50, SizeBytes: 1, Digest: "d"})
	s.ApproveQuality(QualityCheck{ListingID: "s1", Passed: true})
	s.RecordDownload("s1")

	s.AddReview(Review{ID: "r1", ListingID: "s1", Author: "carol", Rating: 5})

	stats := s.Stats()
	if stats.TotalListings != 2 {
		t.Errorf("total = %d, want 2", stats.TotalListings)
	}
	if stats.ApprovedListings != 1 {
		t.Errorf("approved = %d, want 1", stats.ApprovedListings)
	}
	if stats.TotalDownloads != 1 {
		t.Errorf("downloads = %d, want 1", stats.TotalDownloads)
	}
	if stats.UniqueCreators != 2 {
		t.Errorf("creators = %d, want 2", stats.UniqueCreators)
	}
	if stats.TotalReviews != 1 {
		t.Errorf("reviews = %d, want 1", stats.TotalReviews)
	}
}

// ─── containsIgnoreCase Tests ───────────────────────────────────────────────

func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		s, substr string
		want      bool
	}{
		{"Hello World", "hello", true},
		{"Hello World", "WORLD", true},
		{"abc", "abcd", false},
		{"abc", "", true},
		{"", "a", false},
		{"CodeAssistant", "assist", true},
	}
	for _, tt := range tests {
		if got := containsIgnoreCase(tt.s, tt.substr); got != tt.want {
			t.Errorf("containsIgnoreCase(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
		}
	}
}

// ─── Concurrent Safety Tests ────────────────────────────────────────────────

func TestStore_ConcurrentPublishAndSearch(t *testing.T) {
	s := NewStore(StoreConfig{
		MaxListingsPerCreator: 100,
		MinPrice:              1,
		MaxPrice:              10000,
		CreatorSharePct:       85,
	})

	done := make(chan struct{})

	// Publishers
	for i := 0; i < 20; i++ {
		go func(id int) {
			s.Publish(Listing{
				ID:        fmt.Sprintf("c-%d", id),
				Creator:   fmt.Sprintf("creator-%d", id%5),
				ModelName: fmt.Sprintf("Model %d", id),
				Price:     int64(id + 1),
				SizeBytes: 1,
				Digest:    "d",
			})
			done <- struct{}{}
		}(i)
	}

	// Searchers (concurrent with publishers)
	for i := 0; i < 10; i++ {
		go func() {
			s.Search("", "Model")
			done <- struct{}{}
		}()
	}

	for i := 0; i < 30; i++ {
		<-done
	}
	// No panics = pass
}
