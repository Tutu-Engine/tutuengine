// Package marketplace implements the TuTU model marketplace.
// Phase 4 spec: "Community publishes fine-tuned models → earn credits
// per download. Review system: ratings, verified benchmarks. Curation:
// automated quality checks."
//
// How the marketplace works for beginners:
//  1. A creator fine-tunes a model using the finetune package
//  2. They publish it to the marketplace with metadata + benchmarks
//  3. Other users browse/search, check reviews, then download
//  4. Creator earns credits per download
//  5. Automated quality checks verify the model isn't corrupted/malicious
package marketplace

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ─── Errors ─────────────────────────────────────────────────────────────────

var (
	ErrListingNotFound   = errors.New("marketplace listing not found")
	ErrAlreadyPublished  = errors.New("model already published")
	ErrReviewNotFound    = errors.New("review not found")
	ErrSelfReview        = errors.New("cannot review your own model")
	ErrDuplicateReview   = errors.New("already reviewed this model")
	ErrModelUnverified   = errors.New("model has not passed quality checks")
	ErrInsufficientFunds = errors.New("insufficient credits for download")
)

// ─── Listing Types ──────────────────────────────────────────────────────────

// ListingStatus tracks the lifecycle of a published model.
type ListingStatus string

const (
	StatusDraft      ListingStatus = "DRAFT"     // Not yet published
	StatusPending    ListingStatus = "PENDING"   // Awaiting quality check
	StatusApproved   ListingStatus = "APPROVED"  // Passed checks, visible
	StatusRejected   ListingStatus = "REJECTED"  // Failed quality checks
	StatusDelisted   ListingStatus = "DELISTED"  // Removed by creator/admin
)

// Category classifies models for browsing.
type Category string

const (
	CatGeneral    Category = "general"
	CatCode       Category = "code"
	CatCreative   Category = "creative"
	CatScience    Category = "science"
	CatTranslator Category = "translator"
	CatChat       Category = "chat"
)

// Listing represents a model published in the marketplace.
type Listing struct {
	ID           string        `json:"id"`
	ModelName    string        `json:"model_name"`     // Human-readable name
	BaseModel    string        `json:"base_model"`     // What it was fine-tuned from
	Creator      string        `json:"creator"`        // Publisher's node/user ID
	Description  string        `json:"description"`
	Category     Category      `json:"category"`
	Tags         []string      `json:"tags"`
	Version      string        `json:"version"`
	SizeBytes    int64         `json:"size_bytes"`
	Digest       string        `json:"digest"`         // SHA-256 of model file
	Status       ListingStatus `json:"status"`
	Price        int64         `json:"price"`          // Credits per download
	Downloads    int64         `json:"downloads"`
	TotalRevenue int64         `json:"total_revenue"`  // Credits earned
	CreatedAt    time.Time     `json:"created_at"`
	PublishedAt  time.Time     `json:"published_at,omitempty"`
	Benchmarks   Benchmarks    `json:"benchmarks"`
}

// Benchmarks holds verified performance metrics for a listed model.
type Benchmarks struct {
	Perplexity     float64 `json:"perplexity,omitempty"`     // Lower is better
	BLEU           float64 `json:"bleu,omitempty"`           // 0-100, higher better
	HumanEval      float64 `json:"human_eval,omitempty"`     // Code pass@1
	TokPerSec      float64 `json:"tok_per_sec,omitempty"`    // Generation speed
	MemoryMB       int64   `json:"memory_mb,omitempty"`      // VRAM required
	ContextLength  int     `json:"context_length,omitempty"` // Max tokens
	Verified       bool    `json:"verified"`                 // Benchmarks independently verified
}

// ─── Review ─────────────────────────────────────────────────────────────────

// Review is a user review of a marketplace listing.
type Review struct {
	ID        string    `json:"id"`
	ListingID string    `json:"listing_id"`
	Author    string    `json:"author"`    // Reviewer's node/user ID
	Rating    int       `json:"rating"`    // 1-5 stars
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

// Validate checks review fields.
func (r *Review) Validate() error {
	if r.Rating < 1 || r.Rating > 5 {
		return fmt.Errorf("rating must be 1-5, got %d", r.Rating)
	}
	if r.ListingID == "" || r.Author == "" {
		return fmt.Errorf("listing_id and author are required")
	}
	return nil
}

// ─── Quality Check ──────────────────────────────────────────────────────────

// QualityCheck records the result of automated model validation.
type QualityCheck struct {
	ListingID   string    `json:"listing_id"`
	Passed      bool      `json:"passed"`
	Issues      []string  `json:"issues,omitempty"`  // Reasons for failure
	CheckedAt   time.Time `json:"checked_at"`
	Signatures  bool      `json:"signatures"`   // Digital signature valid
	NoMalware   bool      `json:"no_malware"`   // No suspicious patterns
	Benchmarked bool      `json:"benchmarked"`  // Benchmarks reproduced
}

// ─── Marketplace Store ──────────────────────────────────────────────────────

// StoreConfig configures the marketplace.
type StoreConfig struct {
	MaxListingsPerCreator int   // Max models one creator can publish
	MinPrice              int64 // Minimum download price in credits
	MaxPrice              int64 // Maximum download price in credits
	CreatorSharePct       int   // Percentage of download price going to creator (rest is platform fee)
}

// DefaultStoreConfig returns production defaults.
func DefaultStoreConfig() StoreConfig {
	return StoreConfig{
		MaxListingsPerCreator: 50,
		MinPrice:              1,
		MaxPrice:              10000,
		CreatorSharePct:       85, // 85% to creator, 15% platform fee
	}
}

// Store manages all marketplace listings, reviews, and quality checks.
type Store struct {
	mu       sync.RWMutex
	config   StoreConfig
	listings map[string]*Listing        // id → listing
	reviews  map[string][]*Review       // listingID → reviews
	checks   map[string]*QualityCheck   // listingID → latest quality check
}

// NewStore creates a marketplace store.
func NewStore(cfg StoreConfig) *Store {
	return &Store{
		config:   cfg,
		listings: make(map[string]*Listing),
		reviews:  make(map[string][]*Review),
		checks:   make(map[string]*QualityCheck),
	}
}

// Publish adds a new listing to the marketplace.
func (s *Store) Publish(listing Listing) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.listings[listing.ID]; exists {
		return ErrAlreadyPublished
	}

	// Enforce per-creator limit
	count := 0
	for _, l := range s.listings {
		if l.Creator == listing.Creator {
			count++
		}
	}
	if count >= s.config.MaxListingsPerCreator {
		return fmt.Errorf("creator %s reached max listings (%d)", listing.Creator, s.config.MaxListingsPerCreator)
	}

	// Validate price
	if listing.Price < s.config.MinPrice || listing.Price > s.config.MaxPrice {
		return fmt.Errorf("price %d outside allowed range [%d, %d]", listing.Price, s.config.MinPrice, s.config.MaxPrice)
	}

	listing.Status = StatusPending
	listing.CreatedAt = time.Now()
	listing.Downloads = 0
	listing.TotalRevenue = 0

	s.listings[listing.ID] = &listing
	return nil
}

// GetListing returns a listing by ID.
func (s *Store) GetListing(id string) (*Listing, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	l, ok := s.listings[id]
	if !ok {
		return nil, ErrListingNotFound
	}
	cp := *l
	return &cp, nil
}

// Search finds listings matching category and/or text query.
// Returns only APPROVED listings sorted by downloads (most popular first).
func (s *Store) Search(category Category, query string) []Listing {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []Listing
	for _, l := range s.listings {
		if l.Status != StatusApproved {
			continue
		}
		if category != "" && l.Category != category {
			continue
		}
		if query != "" && !containsIgnoreCase(l.ModelName, query) &&
			!containsIgnoreCase(l.Description, query) {
			continue
		}
		results = append(results, *l)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Downloads > results[j].Downloads
	})
	return results
}

// ListByCreator returns all listings from a creator.
func (s *Store) ListByCreator(creator string) []Listing {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []Listing
	for _, l := range s.listings {
		if l.Creator == creator {
			results = append(results, *l)
		}
	}
	return results
}

// ApproveQuality records a quality check result and updates listing status.
func (s *Store) ApproveQuality(check QualityCheck) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	l, ok := s.listings[check.ListingID]
	if !ok {
		return ErrListingNotFound
	}

	check.CheckedAt = time.Now()
	s.checks[check.ListingID] = &check

	if check.Passed {
		l.Status = StatusApproved
		l.PublishedAt = time.Now()
	} else {
		l.Status = StatusRejected
	}
	return nil
}

// RecordDownload increments download count and calculates revenue.
// Returns the credits earned by the creator (creator share).
func (s *Store) RecordDownload(listingID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	l, ok := s.listings[listingID]
	if !ok {
		return 0, ErrListingNotFound
	}

	if l.Status != StatusApproved {
		return 0, ErrModelUnverified
	}

	l.Downloads++
	creatorShare := l.Price * int64(s.config.CreatorSharePct) / 100
	l.TotalRevenue += creatorShare

	return creatorShare, nil
}

// AddReview adds a review to a listing.
func (s *Store) AddReview(review Review) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	l, ok := s.listings[review.ListingID]
	if !ok {
		return ErrListingNotFound
	}

	if review.Author == l.Creator {
		return ErrSelfReview
	}

	// Check for duplicate review
	for _, r := range s.reviews[review.ListingID] {
		if r.Author == review.Author {
			return ErrDuplicateReview
		}
	}

	if err := review.Validate(); err != nil {
		return err
	}

	review.CreatedAt = time.Now()
	s.reviews[review.ListingID] = append(s.reviews[review.ListingID], &review)
	return nil
}

// Reviews returns all reviews for a listing.
func (s *Store) Reviews(listingID string) []Review {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Review
	for _, r := range s.reviews[listingID] {
		result = append(result, *r)
	}
	return result
}

// AverageRating returns the average star rating for a listing.
func (s *Store) AverageRating(listingID string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	reviews := s.reviews[listingID]
	if len(reviews) == 0 {
		return 0
	}

	var total int
	for _, r := range reviews {
		total += r.Rating
	}
	return float64(total) / float64(len(reviews))
}

// DelistListing removes a listing from the marketplace.
func (s *Store) DelistListing(listingID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	l, ok := s.listings[listingID]
	if !ok {
		return ErrListingNotFound
	}
	l.Status = StatusDelisted
	return nil
}

// Stats returns aggregate marketplace statistics.
func (s *Store) Stats() MarketplaceStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stats MarketplaceStats
	creators := make(map[string]bool)
	for _, l := range s.listings {
		stats.TotalListings++
		if l.Status == StatusApproved {
			stats.ApprovedListings++
		}
		stats.TotalDownloads += l.Downloads
		stats.TotalRevenue += l.TotalRevenue
		creators[l.Creator] = true
	}
	stats.UniqueCreators = len(creators)

	for _, reviews := range s.reviews {
		stats.TotalReviews += len(reviews)
	}
	return stats
}

// MarketplaceStats holds aggregate marketplace data.
type MarketplaceStats struct {
	TotalListings    int   `json:"total_listings"`
	ApprovedListings int   `json:"approved_listings"`
	TotalDownloads   int64 `json:"total_downloads"`
	TotalRevenue     int64 `json:"total_revenue"`
	TotalReviews     int   `json:"total_reviews"`
	UniqueCreators   int   `json:"unique_creators"`
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// containsIgnoreCase checks if s contains substr (case-insensitive).
func containsIgnoreCase(s, substr string) bool {
	sl := len(s)
	tl := len(substr)
	if tl > sl {
		return false
	}
	for i := 0; i <= sl-tl; i++ {
		match := true
		for j := 0; j < tl; j++ {
			sc := s[i+j]
			tc := substr[j]
			if sc >= 'A' && sc <= 'Z' {
				sc += 32
			}
			if tc >= 'A' && tc <= 'Z' {
				tc += 32
			}
			if sc != tc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
