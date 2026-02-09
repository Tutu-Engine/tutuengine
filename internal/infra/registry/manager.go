package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/catalog"
	"github.com/tutu-network/tutu/internal/infra/sqlite"
)

// Manager implements domain.ModelManager.
// It manages content-addressed blobs in a local directory and tracks
// metadata in SQLite.
type Manager struct {
	dir       string // Root models directory (contains blobs/ and manifests/)
	db        *sqlite.DB
	urlOverride string // If set, use this base URL instead of HuggingFace (for testing)
}

// NewManager creates a Manager rooted at dir.
func NewManager(dir string, db *sqlite.DB) *Manager {
	return &Manager{dir: dir, db: db}
}

// SetTestURL sets a URL override for testing (downloads go to this URL instead of HuggingFace).
func (m *Manager) SetTestURL(url string) { m.urlOverride = url }

// Init ensures the directory structure exists.
func (m *Manager) Init() error {
	dirs := []string{
		filepath.Join(m.dir, "blobs"),
		filepath.Join(m.dir, "manifests"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}

// BlobPath returns the filesystem path for a content-addressed blob.
func (m *Manager) BlobPath(digest string) string {
	// digest is "sha256:<hex>" → store as blobs/sha256-<hex>
	safe := strings.ReplaceAll(digest, ":", "-")
	return filepath.Join(m.dir, "blobs", safe)
}

// ManifestPath returns the path for a model manifest file.
func (m *Manager) ManifestPath(ref domain.ModelRef) string {
	name := ref.Name
	tag := ref.Tag
	if tag == "" {
		tag = "latest"
	}
	return filepath.Join(m.dir, "manifests", name, tag)
}

// HasLocal checks whether a model exists locally.
func (m *Manager) HasLocal(ref domain.ModelRef) (bool, error) {
	info, err := m.db.GetModel(ref.String())
	if err != nil {
		return false, err
	}
	return info != nil, nil
}

// Resolve returns the path to the primary weights blob for a model.
// This is used by the engine pool to load a model.
func (m *Manager) Resolve(name string) (string, error) {
	ref := ParseRef(name)

	info, err := m.db.GetModel(ref.String())
	if err != nil {
		return "", fmt.Errorf("query model %s: %w", ref, err)
	}
	if info == nil {
		return "", domain.ErrModelNotFound
	}

	// Touch to update last-used
	_ = m.db.TouchModel(ref.String())

	// Load manifest
	manifest, err := m.loadManifest(ref)
	if err != nil {
		return "", err
	}

	// Find the weights layer (typically the largest layer or type "model")
	for _, layer := range manifest.Layers {
		if layer.MediaType == "application/vnd.tutu.model" ||
			strings.Contains(layer.MediaType, "model") ||
			strings.HasSuffix(layer.Digest, ".gguf") {
			path := m.BlobPath(layer.Digest)
			if _, err := os.Stat(path); err != nil {
				return "", fmt.Errorf("blob missing for %s: %w", layer.Digest, domain.ErrModelCorrupted)
			}
			return path, nil
		}
	}

	// Fallback: return first layer
	if len(manifest.Layers) > 0 {
		path := m.BlobPath(manifest.Layers[0].Digest)
		return path, nil
	}

	return "", fmt.Errorf("model %s has no layers: %w", ref, domain.ErrModelCorrupted)
}

// List returns all locally stored models.
func (m *Manager) List() ([]domain.ModelInfo, error) {
	return m.db.ListModels()
}

// Remove deletes a model from local storage.
func (m *Manager) Remove(name string) error {
	ref := ParseRef(name)

	// Load manifest to find blobs
	manifest, err := m.loadManifest(ref)
	if err == nil {
		// Best-effort blob cleanup
		for _, layer := range manifest.Layers {
			_ = os.Remove(m.BlobPath(layer.Digest))
		}
	}

	// Remove manifest file
	mpath := m.ManifestPath(ref)
	_ = os.Remove(mpath)

	// Remove from DB
	return m.db.DeleteModel(ref.String())
}

// Show returns detailed info about a model.
func (m *Manager) Show(name string) (*domain.ModelInfo, error) {
	ref := ParseRef(name)
	info, err := m.db.GetModel(ref.String())
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, domain.ErrModelNotFound
	}
	return info, nil
}

// Pull downloads a real GGUF model from HuggingFace.
// It streams the file to disk with progress reporting and creates
// the manifest + DB entry once download completes.
func (m *Manager) Pull(name string, progress func(status string, pct float64)) error {
	ref := ParseRef(name)

	if err := m.Init(); err != nil {
		return err
	}

	if progress != nil {
		progress("resolving "+ref.String(), 0)
	}

	// Check if already exists
	exists, err := m.HasLocal(ref)
	if err != nil {
		return err
	}
	if exists {
		if progress != nil {
			progress("already exists", 100)
		}
		return nil
	}

	// Look up in catalog
	entry := catalog.Lookup(ref.String())
	if entry == nil {
		// Also try just the name without tag
		entry = catalog.Lookup(ref.Name)
	}
	if entry == nil {
		// Unknown model: if we have a URL override (test mode), create a synthetic entry
		if m.urlOverride != "" {
			entry = &catalog.ModelEntry{
				Name:         ref.Name,
				Family:       "unknown",
				Parameters:   "unknown",
				Quantization: "unknown",
				Format:       "gguf",
				SizeBytes:    0,
				HFRepo:       "test/test",
				HFFile:       ref.Name + ".gguf",
				Tags:         []string{ref.String()},
			}
		} else {
			return fmt.Errorf("model %q not found in catalog — available models: tinyllama, llama3, phi3, qwen2.5, gemma2, smollm2, mistral\nRun 'tutu list --available' to see all models", ref.String())
		}
	}

	url := entry.DownloadURL()
	if m.urlOverride != "" {
		url = m.urlOverride + "/" + entry.HFFile
	}
	if progress != nil {
		progress(fmt.Sprintf("downloading %s (%s)", entry.Name, domain.HumanSize(entry.SizeBytes)), 0)
	}

	// Download to a temp file first, then rename (atomic)
	tmpPath := filepath.Join(m.dir, "blobs", ".download-"+ref.Name+".tmp")
	if err := os.MkdirAll(filepath.Dir(tmpPath), 0o755); err != nil {
		return err
	}

	// Support resume: check if partial download exists
	var startByte int64
	if stat, err := os.Stat(tmpPath); err == nil {
		startByte = stat.Size()
	}

	// HTTP request with Range header for resume
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "TuTu/0.1.0")
	if startByte > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startByte))
		if progress != nil {
			progress(fmt.Sprintf("resuming from %s", domain.HumanSize(startByte)), float64(startByte)/float64(entry.SizeBytes)*100)
		}
	}

	client := &http.Client{
		Timeout: 0, // No timeout for large downloads
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("download failed: HTTP %d from %s", resp.StatusCode, url)
	}

	// Total size from Content-Length or catalog
	totalSize := entry.SizeBytes
	if resp.ContentLength > 0 {
		totalSize = resp.ContentLength + startByte
	}

	// Open file for writing (append if resuming)
	flags := os.O_CREATE | os.O_WRONLY
	if startByte > 0 && resp.StatusCode == http.StatusPartialContent {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		startByte = 0
	}
	f, err := os.OpenFile(tmpPath, flags, 0o644)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}

	// Stream download with progress
	hasher := sha256.New()
	buf := make([]byte, 256*1024) // 256KB buffer
	downloaded := startByte

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return fmt.Errorf("write file: %w", err)
			}
			hasher.Write(buf[:n])
			downloaded += int64(n)

			if progress != nil && totalSize > 0 {
				pct := float64(downloaded) / float64(totalSize) * 100
				speed := domain.HumanSize(downloaded)
				progress(fmt.Sprintf("downloading %s / %s", speed, domain.HumanSize(totalSize)), pct)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			f.Close()
			return fmt.Errorf("download interrupted: %w — run 'tutu pull %s' to resume", readErr, name)
		}
	}
	f.Close()

	// Compute SHA256 of the full file (for content addressing)
	digest, err := hashFile(tmpPath)
	if err != nil {
		return fmt.Errorf("hash file: %w", err)
	}
	fullDigest := "sha256:" + digest

	if progress != nil {
		progress("verifying download", 99)
	}

	// Move to final content-addressed location
	blobPath := m.BlobPath(fullDigest)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, blobPath); err != nil {
		// Cross-device? Copy instead
		if copyErr := copyFile(tmpPath, blobPath); copyErr != nil {
			return fmt.Errorf("move blob: %w", copyErr)
		}
		os.Remove(tmpPath)
	}

	// Get actual file size
	stat, err := os.Stat(blobPath)
	if err != nil {
		return err
	}

	// Create manifest
	manifest := domain.Manifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.tutu.manifest.v1+json",
		Layers: []domain.Layer{
			{
				MediaType: "application/vnd.tutu.model",
				Digest:    fullDigest,
				Size:      stat.Size(),
			},
		},
	}

	if err := m.saveManifest(ref, manifest); err != nil {
		return err
	}

	// Store in DB with real metadata
	now := time.Now()
	info := domain.ModelInfo{
		Name:         ref.String(),
		SizeBytes:    stat.Size(),
		Digest:       fullDigest,
		PulledAt:     now,
		Format:       entry.Format,
		Family:       entry.Family,
		Parameters:   entry.Parameters,
		Quantization: entry.Quantization,
	}
	if err := m.db.UpsertModel(info); err != nil {
		return err
	}

	if progress != nil {
		progress("done", 100)
	}
	return nil
}

// hashFile computes SHA256 of a file on disk.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyFile copies src to dst (for cross-device moves).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// CreateFromTuTufile creates a model from a TuTufile.
func (m *Manager) CreateFromTuTufile(name string, tf domain.TuTufile) error {
	ref := ParseRef(name)

	if err := m.Init(); err != nil {
		return err
	}

	// Use the base model if FROM is specified (for now, just record it)
	blobContent := []byte(fmt.Sprintf("TUTU-CUSTOM-MODEL:%s:FROM:%s\n", ref.String(), tf.From))
	digest := "sha256:" + computeSHA256(blobContent)

	blobPath := m.BlobPath(digest)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(blobPath, blobContent, 0o644); err != nil {
		return err
	}

	layers := []domain.Layer{
		{
			MediaType: "application/vnd.tutu.model",
			Digest:    digest,
			Size:      int64(len(blobContent)),
		},
	}

	// Store system prompt as a layer if present
	if tf.System != "" {
		sysContent := []byte(tf.System)
		sysDigest := "sha256:" + computeSHA256(sysContent)
		sysPath := m.BlobPath(sysDigest)
		if err := os.WriteFile(sysPath, sysContent, 0o644); err != nil {
			return err
		}
		layers = append(layers, domain.Layer{
			MediaType: "application/vnd.tutu.system-prompt",
			Digest:    sysDigest,
			Size:      int64(len(sysContent)),
		})
	}

	manifest := domain.Manifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.tutu.manifest.v1+json",
		Layers:        layers,
	}

	if err := m.saveManifest(ref, manifest); err != nil {
		return err
	}

	totalSize := int64(0)
	for _, l := range layers {
		totalSize += l.Size
	}

	now := time.Now()
	info := domain.ModelInfo{
		Name:      ref.String(),
		SizeBytes: totalSize,
		Digest:    digest,
		PulledAt:  now,
		Format:    "gguf",
	}
	return m.db.UpsertModel(info)
}

// --- Internal helpers ---

func (m *Manager) loadManifest(ref domain.ModelRef) (domain.Manifest, error) {
	mpath := m.ManifestPath(ref)
	data, err := os.ReadFile(mpath)
	if err != nil {
		return domain.Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var manifest domain.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return domain.Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	return manifest, nil
}

func (m *Manager) saveManifest(ref domain.ModelRef, manifest domain.Manifest) error {
	mpath := m.ManifestPath(ref)
	if err := os.MkdirAll(filepath.Dir(mpath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(mpath, data, 0o644)
}

// ParseRef parses a "name:tag" string into a ModelRef.
func ParseRef(s string) domain.ModelRef {
	parts := strings.SplitN(s, ":", 2)
	ref := domain.ModelRef{Name: parts[0]}
	if len(parts) == 2 {
		ref.Tag = parts[1]
	} else {
		ref.Tag = "latest"
	}
	return ref
}

func computeSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
