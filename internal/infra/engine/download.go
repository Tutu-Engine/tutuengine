// Package engine — auto-download llama-server from llama.cpp releases.
// When the user runs `tutu run <model>` and llama-server is not found,
// TuTu automatically downloads the correct binary for the platform.
package engine

import (
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// llamaCppReleasesAPI is the GitHub API endpoint for llama.cpp releases.
const llamaCppReleasesAPI = "https://api.github.com/repos/ggml-org/llama.cpp/releases/latest"

// DownloadLlamaServer downloads the llama-server binary from the latest
// llama.cpp release and places it in tutuHome/bin/.
// Returns the path to the downloaded binary on success.
func DownloadLlamaServer(tutuHome string, progress func(status string, pct float64)) (string, error) {
	binDir := filepath.Join(tutuHome, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("create bin dir: %w", err)
	}

	exe := "llama-server"
	if runtime.GOOS == "windows" {
		exe = "llama-server.exe"
	}
	targetPath := filepath.Join(binDir, exe)

	// Already exists?
	if _, err := os.Stat(targetPath); err == nil {
		return targetPath, nil
	}

	if progress != nil {
		progress("finding latest llama.cpp release...", 0)
	}

	// Get latest release info from GitHub
	assetURL, assetName, err := findLlamaServerAsset()
	if err != nil {
		return "", fmt.Errorf("find llama-server release: %w", err)
	}

	if progress != nil {
		progress(fmt.Sprintf("downloading %s...", assetName), 5)
	}

	// Download the asset
	tmpPath := filepath.Join(binDir, ".download-llama-server.tmp")
	if err := downloadFile(assetURL, tmpPath, progress); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("download llama-server: %w", err)
	}

	if progress != nil {
		progress("extracting llama-server...", 90)
	}

	// Extract the binary from the archive
	if err := extractLlamaServer(tmpPath, targetPath, assetName); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("extract llama-server: %w", err)
	}
	os.Remove(tmpPath)

	// Make executable on Unix
	if runtime.GOOS != "windows" {
		os.Chmod(targetPath, 0o755)
	}

	if progress != nil {
		progress("llama-server ready!", 100)
	}

	return targetPath, nil
}

// findLlamaServerAsset queries the GitHub API for the latest llama.cpp release
// and returns the download URL and filename for the current platform.
func findLlamaServerAsset() (url, name string, err error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", llamaCppReleasesAPI, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "TuTu/0.1.0")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", fmt.Errorf("parse release JSON: %w", err)
	}

	// Build the pattern we're looking for based on OS/arch
	patterns := platformPatterns()

	// Score assets — prefer exact matches
	for _, pattern := range patterns {
		for _, asset := range release.Assets {
			nameLower := strings.ToLower(asset.Name)
			if matchesAsset(nameLower, pattern) {
				return asset.BrowserDownloadURL, asset.Name, nil
			}
		}
	}

	// If no match found, list available assets for debugging
	available := make([]string, 0, len(release.Assets))
	for _, a := range release.Assets {
		available = append(available, a.Name)
	}
	return "", "", fmt.Errorf(
		"no llama-server binary found for %s/%s in release %s\nAvailable assets: %s",
		runtime.GOOS, runtime.GOARCH, release.TagName,
		strings.Join(available, ", "),
	)
}

// platformPatterns returns search patterns for the current OS/arch.
// The patterns are tried in order — first match wins.
func platformPatterns() []assetPattern {
	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return []assetPattern{
				// Prefer CUDA builds, then Vulkan, then CPU
				{mustContain: []string{"win", "x64"}, mustNotContain: []string{"arm", "sha1", "sha256"}},
				{mustContain: []string{"win", "amd64"}, mustNotContain: []string{"arm", "sha1", "sha256"}},
				{mustContain: []string{"win"}, mustNotContain: []string{"arm", "sha1", "sha256"}},
			}
		case "arm64":
			return []assetPattern{
				{mustContain: []string{"win", "arm64"}, mustNotContain: []string{"sha1", "sha256"}},
			}
		}
	case "darwin":
		switch runtime.GOARCH {
		case "arm64":
			return []assetPattern{
				{mustContain: []string{"macos", "arm64"}, mustNotContain: []string{"sha1", "sha256"}},
				{mustContain: []string{"darwin", "arm64"}, mustNotContain: []string{"sha1", "sha256"}},
				{mustContain: []string{"macos"}, mustNotContain: []string{"x86", "x64", "amd64", "sha1", "sha256"}},
			}
		case "amd64":
			return []assetPattern{
				{mustContain: []string{"macos", "x64"}, mustNotContain: []string{"arm", "sha1", "sha256"}},
				{mustContain: []string{"darwin", "x86_64"}, mustNotContain: []string{"arm", "sha1", "sha256"}},
				{mustContain: []string{"macos"}, mustNotContain: []string{"arm", "sha1", "sha256"}},
			}
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return []assetPattern{
				{mustContain: []string{"linux", "x64"}, mustNotContain: []string{"arm", "sha1", "sha256", "cuda", "vulkan"}},
				{mustContain: []string{"linux", "amd64"}, mustNotContain: []string{"arm", "sha1", "sha256", "cuda", "vulkan"}},
				{mustContain: []string{"linux"}, mustNotContain: []string{"arm", "sha1", "sha256", "cuda", "vulkan"}},
			}
		case "arm64":
			return []assetPattern{
				{mustContain: []string{"linux", "arm64"}, mustNotContain: []string{"sha1", "sha256"}},
				{mustContain: []string{"linux", "aarch64"}, mustNotContain: []string{"sha1", "sha256"}},
			}
		}
	}

	// Fallback — try anything with the OS name
	return []assetPattern{
		{mustContain: []string{runtime.GOOS}, mustNotContain: []string{"sha1", "sha256"}},
	}
}

type assetPattern struct {
	mustContain    []string
	mustNotContain []string
}

func matchesAsset(name string, p assetPattern) bool {
	for _, s := range p.mustContain {
		if !strings.Contains(name, s) {
			return false
		}
	}
	for _, s := range p.mustNotContain {
		if strings.Contains(name, s) {
			return false
		}
	}
	// Must be an archive (zip or tar.gz)
	return strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tar.gz")
}

// downloadFile downloads a URL to a local file with progress reporting.
func downloadFile(url, dst string, progress func(string, float64)) error {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "TuTu/0.1.0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	totalSize := resp.ContentLength
	buf := make([]byte, 256*1024)
	var downloaded int64

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			downloaded += int64(n)
			if progress != nil && totalSize > 0 {
				pct := 5.0 + (float64(downloaded)/float64(totalSize))*80.0 // 5-85%
				mb := float64(downloaded) / (1024 * 1024)
				totalMB := float64(totalSize) / (1024 * 1024)
				progress(fmt.Sprintf("downloading llama-server: %.0f MB / %.0f MB", mb, totalMB), pct)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

// extractLlamaServer extracts the llama-server binary from a zip or tar.gz archive.
func extractLlamaServer(archivePath, targetPath, archiveName string) error {
	if strings.HasSuffix(strings.ToLower(archiveName), ".zip") {
		return extractFromZip(archivePath, targetPath)
	}
	if strings.HasSuffix(strings.ToLower(archiveName), ".tar.gz") {
		return extractFromTarGz(archivePath, targetPath)
	}
	return fmt.Errorf("unsupported archive format: %s", archiveName)
}

// extractFromZip extracts llama-server from a zip archive.
func extractFromZip(archivePath, targetPath string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	targetName := "llama-server"
	if runtime.GOOS == "windows" {
		targetName = "llama-server.exe"
	}

	for _, f := range r.File {
		name := filepath.Base(f.Name)
		if strings.EqualFold(name, targetName) {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			out, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			defer out.Close()

			_, err = io.Copy(out, rc)
			return err
		}
	}

	return fmt.Errorf("llama-server binary not found in archive (looked for %s)", targetName)
}

// extractFromTarGz extracts llama-server from a .tar.gz archive.
func extractFromTarGz(archivePath, targetPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()

	return extractFromTar(gr, targetPath)
}
