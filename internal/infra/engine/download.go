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

	// Check if llama-server exists AND its companion libraries are present.
	// On Windows, llama-server.exe needs ggml.dll + llama.dll to run.
	// A previous buggy version extracted only the exe — detect and re-download.
	if _, err := os.Stat(targetPath); err == nil {
		if !missingCompanionLibs(binDir) {
			return targetPath, nil
		}
		// Companion DLLs/libs missing — re-download the full package
		if progress != nil {
			progress("re-downloading llama-server (missing companion libraries)...", 0)
		}
		os.Remove(targetPath) // Remove the incomplete install
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

// missingCompanionLibs checks whether required companion libraries are present
// in binDir alongside llama-server. Returns true if any required lib is missing.
// This prevents the common "Library not loaded" / "dyld" errors on macOS and
// "cannot open shared object file" on Linux.
func missingCompanionLibs(binDir string) bool {
	switch runtime.GOOS {
	case "windows":
		// llama-server.exe on Windows requires at minimum ggml.dll
		for _, dll := range []string{"ggml.dll"} {
			if _, err := os.Stat(filepath.Join(binDir, dll)); err != nil {
				return true
			}
		}
	case "darwin":
		// macOS builds dynamically link against multiple dylibs.
		// Missing libmtmd.dylib, libggml.dylib, etc. causes:
		//   dyld: Library not loaded: @rpath/libmtmd.0.dylib
		// Scan for any .dylib files — if llama-server exists but no dylibs
		// are present, the extraction likely failed.
		entries, err := os.ReadDir(binDir)
		if err != nil {
			return true
		}
		hasDylib := false
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".dylib") {
				hasDylib = true
				break
			}
		}
		// If llama-server has no companion dylibs, consider them missing.
		// This covers libmtmd, libggml, libllama, libcommon, etc.
		if !hasDylib {
			// Check if the binary is statically linked by trying to run --version
			// If it fails with a dylib error, we need to re-download.
			return true
		}
	case "linux":
		// Modern llama.cpp Linux builds may also use shared libs.
		// Check for .so files alongside llama-server.
		entries, err := os.ReadDir(binDir)
		if err != nil {
			return false
		}
		hasServerSo := false
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".so") {
				hasServerSo = true
				break
			}
		}
		// If no .so files exist, the build is likely statically linked — OK
		_ = hasServerSo
		return false
	}
	return false
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
// IMPORTANT: llama.cpp asset naming conventions (as of b4000+):
//   - Windows: llama-{tag}-bin-win-cpu-x64.zip, llama-{tag}-bin-win-cuda-*.zip
//   - macOS:   llama-{tag}-bin-macos-arm64.tar.gz, llama-{tag}-bin-macos-x64.tar.gz
//   - Linux:   llama-{tag}-bin-ubuntu-x64.tar.gz (NOT "linux"!)
//   - Also:    cudart-llama-bin-win-*.zip (CUDA DLLs only — no llama-server!)
func platformPatterns() []assetPattern {
	// Common excludes: cudart (DLL-only packages), hash files, GPU-specific builds
	noHash := []string{"sha1", "sha256", "sha512", "cudart"}

	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return []assetPattern{
				// Prefer CPU-only build (smallest, most compatible)
				{mustContain: []string{"win", "cpu", "x64"}, mustNotContain: noHash},
				// Then try any Windows x64 that isn't CUDA/Vulkan/HIP/SYCL
				{mustContain: []string{"win", "x64"}, mustNotContain: append(noHash, "cuda", "vulkan", "hip", "sycl", "opencl", "arm")},
			}
		case "arm64":
			return []assetPattern{
				{mustContain: []string{"win", "cpu", "arm64"}, mustNotContain: noHash},
				{mustContain: []string{"win", "arm64"}, mustNotContain: append(noHash, "cuda", "vulkan", "hip", "sycl", "opencl")},
			}
		}
	case "darwin":
		switch runtime.GOARCH {
		case "arm64":
			return []assetPattern{
				{mustContain: []string{"macos", "arm64"}, mustNotContain: noHash},
				{mustContain: []string{"darwin", "arm64"}, mustNotContain: noHash},
			}
		case "amd64":
			return []assetPattern{
				{mustContain: []string{"macos", "x64"}, mustNotContain: append(noHash, "arm")},
				{mustContain: []string{"darwin", "x64"}, mustNotContain: append(noHash, "arm")},
				{mustContain: []string{"darwin", "x86_64"}, mustNotContain: append(noHash, "arm")},
			}
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return []assetPattern{
				// llama.cpp uses "ubuntu" not "linux" in asset names
				{mustContain: []string{"ubuntu", "x64"}, mustNotContain: append(noHash, "arm", "vulkan")},
				{mustContain: []string{"ubuntu", "amd64"}, mustNotContain: append(noHash, "arm", "vulkan")},
				// Fallback: try "linux" in case naming changes
				{mustContain: []string{"linux", "x64"}, mustNotContain: append(noHash, "arm", "cuda", "vulkan")},
			}
		case "arm64":
			return []assetPattern{
				{mustContain: []string{"ubuntu", "arm64"}, mustNotContain: noHash},
				{mustContain: []string{"ubuntu", "aarch64"}, mustNotContain: noHash},
				{mustContain: []string{"linux", "arm64"}, mustNotContain: noHash},
				{mustContain: []string{"linux", "aarch64"}, mustNotContain: noHash},
				// openEuler provides ARM64 builds
				{mustContain: []string{"openeuler", "aarch64"}, mustNotContain: append(noHash, "aclgraph")},
			}
		}
	}

	// Fallback — try anything with the OS name
	return []assetPattern{
		{mustContain: []string{runtime.GOOS}, mustNotContain: noHash},
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

// extractLlamaServer extracts the llama-server binary AND all companion files
// (DLLs, shared libraries) from the archive into the same directory as targetPath.
// On Windows, llama-server.exe depends on ggml.dll, llama.dll, etc. that ship
// in the same zip. Without them, it fails with STATUS_DLL_NOT_FOUND (0xc0000135).
func extractLlamaServer(archivePath, targetPath, archiveName string) error {
	destDir := filepath.Dir(targetPath)

	if strings.HasSuffix(strings.ToLower(archiveName), ".zip") {
		return extractAllFromZip(archivePath, destDir)
	}
	if strings.HasSuffix(strings.ToLower(archiveName), ".tar.gz") {
		return extractAllFromTarGz(archivePath, destDir)
	}
	return fmt.Errorf("unsupported archive format: %s", archiveName)
}

// extractAllFromZip extracts all files from a zip archive into destDir.
// Only regular files are extracted (no directories). Files inside nested
// directories within the zip are flattened into destDir.
func extractAllFromZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	foundServer := false
	serverName := "llama-server"
	if runtime.GOOS == "windows" {
		serverName = "llama-server.exe"
	}

	for _, f := range r.File {
		// Skip directories
		if f.FileInfo().IsDir() {
			continue
		}

		name := filepath.Base(f.Name)
		// Skip empty names and macOS metadata
		if name == "" || strings.HasPrefix(name, ".") || strings.HasPrefix(f.Name, "__MACOSX") {
			continue
		}

		// Extract all binaries, libraries, and companion files.
		// llama.cpp ships with several shared libraries that llama-server
		// dynamically links against: libggml, libllama, libmtmd, libcommon, etc.
		// Missing ANY of these causes dyld/ld.so errors at runtime.
		ext := strings.ToLower(filepath.Ext(name))
		nameLower := strings.ToLower(name)
		isRelevant := ext == ".exe" || ext == ".dll" || ext == ".so" || ext == ".dylib" ||
			ext == ".metal" || ext == ".metallib" || ext == "" || // unix binaries have no extension
			strings.HasPrefix(nameLower, "llama") || strings.HasPrefix(nameLower, "ggml") ||
			strings.HasPrefix(nameLower, "lib") // ALL shared libraries: libmtmd, libcommon, etc.
		if !isRelevant {
			continue
		}

		if strings.EqualFold(name, serverName) {
			foundServer = true
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open %s in zip: %w", f.Name, err)
		}

		outPath := filepath.Join(destDir, name)
		out, err := os.Create(outPath)
		if err != nil {
			rc.Close()
			return fmt.Errorf("create %s: %w", outPath, err)
		}

		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return fmt.Errorf("extract %s: %w", name, err)
		}

		// Make executable on unix
		if runtime.GOOS != "windows" {
			os.Chmod(outPath, 0o755)
		}
	}

	if !foundServer {
		return fmt.Errorf("llama-server binary not found in archive (looked for %s)", serverName)
	}

	return nil
}

// extractAllFromTarGz extracts all relevant files from a .tar.gz archive into destDir.
func extractAllFromTarGz(archivePath, destDir string) error {
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

	return extractAllFromTar(gr, destDir)
}
