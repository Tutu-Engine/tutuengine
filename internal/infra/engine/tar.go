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

// extractFromTar extracts the llama-server binary from a tar stream.
func extractFromTar(r io.Reader, targetPath string) error {
	tr := tar.NewReader(r)

	targetName := "llama-server"
	if runtime.GOOS == "windows" {
		targetName = "llama-server.exe"
	}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		name := filepath.Base(hdr.Name)
		if strings.EqualFold(name, targetName) && hdr.Typeflag == tar.TypeReg {
			out, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			defer out.Close()

			_, err = io.Copy(out, tr)
			return err
		}
	}

	return fmt.Errorf("llama-server binary not found in tar archive (looked for %s)", targetName)
}
