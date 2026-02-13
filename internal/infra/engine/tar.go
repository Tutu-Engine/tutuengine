package engine

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// extractAllFromTar extracts all relevant files (binaries, shared libs) from a tar stream
// into destDir. This ensures companion libraries (.so, .dylib) are placed alongside
// llama-server so it can find them at runtime.
func extractAllFromTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)

	serverName := "llama-server"
	if runtime.GOOS == "windows" {
		serverName = "llama-server.exe"
	}

	foundServer := false

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Skip non-regular files
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		name := filepath.Base(hdr.Name)
		if name == "" || strings.HasPrefix(name, ".") {
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

		outPath := filepath.Join(destDir, name)
		out, err := os.Create(outPath)
		if err != nil {
			return fmt.Errorf("create %s: %w", outPath, err)
		}

		_, err = io.Copy(out, tr)
		out.Close()
		if err != nil {
			return fmt.Errorf("extract %s: %w", name, err)
		}

		if runtime.GOOS != "windows" {
			os.Chmod(outPath, 0o755)
		}
	}

	if !foundServer {
		return fmt.Errorf("llama-server binary not found in tar archive (looked for %s)", serverName)
	}

	return nil
}
