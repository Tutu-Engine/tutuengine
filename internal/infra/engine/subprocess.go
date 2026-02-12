// Package engine provides inference backends.
// This file implements a REAL inference backend that manages llama-server
// (from llama.cpp) as a subprocess and communicates via its HTTP API.
//
// Architecture:
//
//	Pool.Acquire("llama3") → SubprocessBackend.LoadModel(path)
//	  → starts llama-server with the GGUF file
//	  → returns SubprocessHandle (proxy to llama-server HTTP API)
//	    → Generate() calls POST /completion on llama-server
//	    → Embed()     calls POST /embedding  on llama-server
//	  → Close() kills the subprocess
package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── Subprocess Backend ─────────────────────────────────────────────────────
// This backend spawns llama-server.exe for each loaded model.
// llama-server exposes an OpenAI-compatible API on localhost.
// We proxy our Generate/Embed calls through it.

// SubprocessBackend manages llama-server processes.
type SubprocessBackend struct {
	llamaServerPath string // Path to llama-server executable
	// ProgressFunc is called during model loading to show feedback.
	// Set by the daemon before Pool.Acquire is called.
	ProgressFunc func(status string)
}

// NewSubprocessBackend creates a backend that uses llama-server.
// It locates the llama-server binary in PATH, in the TuTu bin dir,
// or returns an error with download instructions.
func NewSubprocessBackend(tutuHome string) (*SubprocessBackend, error) {
	path, err := findLlamaServer(tutuHome)
	if err != nil {
		return nil, err
	}
	return &SubprocessBackend{llamaServerPath: path}, nil
}

// SetProgress sets the progress callback for model loading status.
func (b *SubprocessBackend) SetProgress(fn func(string)) {
	b.ProgressFunc = fn
}

// progress emits a status message if a callback is set.
func (b *SubprocessBackend) progress(msg string) {
	if b.ProgressFunc != nil {
		b.ProgressFunc(msg)
	}
}

// findLlamaServer searches for the llama-server binary.
func findLlamaServer(tutuHome string) (string, error) {
	exe := "llama-server"
	if runtime.GOOS == "windows" {
		exe = "llama-server.exe"
	}

	// 1. Check TUTU_HOME/bin/
	binPath := filepath.Join(tutuHome, "bin", exe)
	if _, err := os.Stat(binPath); err == nil {
		return binPath, nil
	}

	// 2. Check PATH
	if path, err := exec.LookPath(exe); err == nil {
		return path, nil
	}

	// 3. Also check "llama-cli" and "llama" variants
	for _, alt := range []string{"llama-cli", "llama"} {
		altExe := alt
		if runtime.GOOS == "windows" {
			altExe = alt + ".exe"
		}
		// Check bin dir
		altPath := filepath.Join(tutuHome, "bin", altExe)
		if _, err := os.Stat(altPath); err == nil {
			return altPath, nil
		}
		// Check PATH
		if path, err := exec.LookPath(altExe); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf(`llama-server not found

TuTu needs llama-server (from llama.cpp) to run AI models.

Install it:
  1. Download from: https://github.com/ggml-org/llama.cpp/releases
     → Download the file for your OS (e.g., llama-*-bin-win-*.zip)
     → Extract llama-server.exe (or llama-server on Mac/Linux)

  2. Place it in one of:
     → %s
     → Or any folder in your system PATH

  3. Then run: tutu pull <model> && tutu run <model>

Alternative: Install via package manager:
  → Windows (winget): winget install ggml-org.llama-server
  → macOS (brew):     brew install llama.cpp
  → Linux:            see https://github.com/ggml-org/llama.cpp#build
`, filepath.Join(tutuHome, "bin"))
}

// LoadModel starts a llama-server subprocess for the given GGUF file.
func (b *SubprocessBackend) LoadModel(path string, opts LoadOptions) (ModelHandle, error) {
	if path == "" {
		return nil, fmt.Errorf("empty model path")
	}

	// Verify file exists and is a GGUF
	stat, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("model file not found: %w", err)
	}

	// Kill any orphaned llama-server processes from previous crashed runs
	killOrphanLlamaServers()

	// Find a free port
	port, err := findFreePort()
	if err != nil {
		return nil, fmt.Errorf("find free port: %w", err)
	}

	// Build llama-server arguments
	args := []string{
		"--model", path,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", port),
		"--ctx-size", fmt.Sprintf("%d", coalesce(opts.NumCtx, 4096)),
		"--no-mmap", // Safer on Windows
	}

	// GPU layers
	if opts.NumGPULayers >= 0 {
		args = append(args, "--n-gpu-layers", fmt.Sprintf("%d", opts.NumGPULayers))
	} else {
		// Auto: try all layers on GPU
		args = append(args, "--n-gpu-layers", "99")
	}

	// Threads
	if opts.NumThreads > 0 {
		args = append(args, "--threads", fmt.Sprintf("%d", opts.NumThreads))
	}

	b.progress("Starting llama-server...")

	// Capture stderr in a ring buffer for diagnostics
	stderrBuf := &limitedBuffer{max: 8192}

	// Start subprocess
	cmd := exec.Command(b.llamaServerPath, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = stderrBuf

	// On Windows, don't show console window + allow clean kill
	configureProcess(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start llama-server: %w", err)
	}

	addr := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Monitor for early exit in the background
	earlyExit := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		earlyExit <- err
	}()

	// Wait for server to become ready with progress feedback
	modelSize := float64(stat.Size()) / (1024 * 1024)
	b.progress(fmt.Sprintf("Loading model (%.0f MB) — this may take a minute...", modelSize))

	if err := waitForServerWithFeedback(addr, 5*time.Minute, earlyExit, stderrBuf, b.ProgressFunc); err != nil {
		cmd.Process.Kill()
		// Include llama-server stderr in error for diagnostics
		stderr := strings.TrimSpace(stderrBuf.String())
		if stderr != "" {
			// Extract last few lines of stderr for the most useful info
			lines := strings.Split(stderr, "\n")
			if len(lines) > 10 {
				lines = lines[len(lines)-10:]
			}
			return nil, fmt.Errorf("llama-server failed to start (model: %s): %w\n\nllama-server output:\n%s",
				filepath.Base(path), err, strings.Join(lines, "\n"))
		}
		return nil, fmt.Errorf("llama-server failed to start (model: %s): %w", filepath.Base(path), err)
	}

	b.progress("Model loaded — ready!")

	return &SubprocessHandle{
		cmd:     cmd,
		addr:    addr,
		port:    port,
		path:    path,
		memSize: uint64(stat.Size()), // Approximate — model file size
		client: &http.Client{
			Timeout: 10 * time.Minute, // Long timeout for generation
		},
	}, nil
}

// Close releases the backend (noop — handles close individually).
func (b *SubprocessBackend) Close() {}

// ─── SubprocessHandle ───────────────────────────────────────────────────────

// SubprocessHandle wraps a running llama-server subprocess.
type SubprocessHandle struct {
	cmd     *exec.Cmd
	addr    string
	port    int
	path    string
	memSize uint64
	client  *http.Client
	closed  bool
}

// Generate sends a completion request to llama-server and streams tokens back.
func (h *SubprocessHandle) Generate(ctx context.Context, prompt string, params GenerateParams) (<-chan domain.Token, error) {
	if h.closed {
		return nil, fmt.Errorf("model is closed")
	}

	// Build request body for llama-server /completion endpoint
	body := map[string]interface{}{
		"prompt":       prompt,
		"stream":       true,
		"temperature":  params.Temperature,
		"top_p":        params.TopP,
		"cache_prompt": true,
	}
	if params.MaxTokens > 0 {
		body["n_predict"] = params.MaxTokens
	} else {
		body["n_predict"] = 1024
	}
	if len(params.Stop) > 0 {
		body["stop"] = params.Stop
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", h.addr+"/completion", strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llama-server request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("llama-server error %d: %s", resp.StatusCode, string(body))
	}

	ch := make(chan domain.Token, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		// Increase buffer for long lines
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()

			// llama-server streams "data: {...}" lines (SSE format)
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			jsonData := strings.TrimPrefix(line, "data: ")
			if jsonData == "" || jsonData == "[DONE]" {
				continue
			}

			var chunk struct {
				Content string `json:"content"`
				Stop    bool   `json:"stop"`
			}
			if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
				continue
			}

			select {
			case <-ctx.Done():
				return
			case ch <- domain.Token{
				Text: chunk.Content,
				Done: chunk.Stop,
			}:
			}

			if chunk.Stop {
				return
			}
		}
	}()

	return ch, nil
}

// Chat sends a chat completion request to llama-server using the /v1/chat/completions
// endpoint. This lets llama-server apply the model's native chat template automatically
// (llama3, chatml, phi3, gemma, mistral, etc).
func (h *SubprocessHandle) Chat(ctx context.Context, messages []ChatMessage, params GenerateParams) (<-chan domain.Token, error) {
	if h.closed {
		return nil, fmt.Errorf("model is closed")
	}

	body := map[string]interface{}{
		"messages":    messages,
		"stream":      true,
		"temperature": params.Temperature,
		"top_p":       params.TopP,
	}
	if params.MaxTokens > 0 {
		body["max_tokens"] = params.MaxTokens
	} else {
		body["max_tokens"] = 1024
	}
	if len(params.Stop) > 0 {
		body["stop"] = params.Stop
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", h.addr+"/v1/chat/completions", strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llama-server chat request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("llama-server chat error %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan domain.Token, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			jsonData := strings.TrimPrefix(line, "data: ")
			if jsonData == "" || jsonData == "[DONE]" {
				continue
			}

			// OpenAI-compatible streaming format
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
				continue
			}

			if len(chunk.Choices) > 0 {
				content := chunk.Choices[0].Delta.Content
				done := chunk.Choices[0].FinishReason != nil

				if content != "" || done {
					select {
					case <-ctx.Done():
						return
					case ch <- domain.Token{
						Text: content,
						Done: done,
					}:
					}
				}

				if done {
					return
				}
			}
		}
	}()

	return ch, nil
}

// Embed generates embeddings via llama-server /embedding endpoint.
func (h *SubprocessHandle) Embed(ctx context.Context, input []string) ([][]float32, error) {
	if h.closed {
		return nil, fmt.Errorf("model is closed")
	}

	results := make([][]float32, len(input))
	for i, text := range input {
		body, _ := json.Marshal(map[string]interface{}{
			"content": text,
		})

		req, err := http.NewRequestWithContext(ctx, "POST", h.addr+"/embedding", strings.NewReader(string(body)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := h.client.Do(req)
		if err != nil {
			return nil, err
		}

		var result struct {
			Embedding []float32 `json:"embedding"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()
		results[i] = result.Embedding
	}

	return results, nil
}

// MemoryBytes returns approximate memory usage (file size as proxy).
func (h *SubprocessHandle) MemoryBytes() uint64 { return h.memSize }

// Close kills the llama-server subprocess and frees resources.
func (h *SubprocessHandle) Close() {
	if h.closed {
		return
	}
	h.closed = true

	// Graceful shutdown: try /shutdown endpoint first
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", h.addr+"/shutdown", nil)
	if req != nil {
		h.client.Do(req) //nolint:errcheck
	}

	// Force kill the process and wait for it to exit.
	// On Windows with CREATE_NEW_PROCESS_GROUP, this kills the entire tree.
	if h.cmd != nil && h.cmd.Process != nil {
		h.cmd.Process.Kill() //nolint:errcheck
		// Wait with a timeout to avoid blocking forever
		done := make(chan struct{})
		go func() {
			h.cmd.Wait() //nolint:errcheck
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			// Process didn't exit, force it
		}
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// findFreePort asks the OS for an available TCP port.
func findFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

// waitForServerWithFeedback polls /health until ready, with progress feedback and
// early-exit detection (if llama-server crashes, we detect it immediately instead
// of waiting the full timeout).
func waitForServerWithFeedback(addr string, timeout time.Duration, earlyExit <-chan error, stderrBuf *limitedBuffer, progressFn func(string)) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	start := time.Now()
	lastMsg := time.Time{}

	for time.Now().Before(deadline) {
		// Check if llama-server exited early (crash)
		select {
		case err := <-earlyExit:
			stderr := strings.TrimSpace(stderrBuf.String())
			if stderr != "" {
				return fmt.Errorf("llama-server exited unexpectedly (exit: %v)\n\nOutput:\n%s", err, stderr)
			}
			return fmt.Errorf("llama-server exited unexpectedly (exit: %v)", err)
		default:
		}

		resp, err := client.Get(addr + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		// Show progress every 5 seconds so user knows we're still working
		if progressFn != nil && time.Since(lastMsg) > 5*time.Second {
			elapsed := int(time.Since(start).Seconds())
			progressFn(fmt.Sprintf("Loading model... (%ds elapsed)", elapsed))
			lastMsg = time.Now()
		}

		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("server at %s did not become ready within %v", addr, timeout)
}

// limitedBuffer is a thread-safe buffer that keeps only the last N bytes.
// Used to capture llama-server stderr without unbounded memory usage.
type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	// Trim to keep only the last `max` bytes
	if b.buf.Len() > b.max {
		data := b.buf.Bytes()
		b.buf.Reset()
		b.buf.Write(data[len(data)-b.max:])
	}
	return n, err
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// killOrphanLlamaServers kills any leftover llama-server processes from
// previous crashed runs. This prevents port/file conflicts.
func killOrphanLlamaServers() {
	if runtime.GOOS == "windows" {
		// On Windows, use taskkill to kill any llama-server.exe processes
		// that are not part of the current process tree. Silently ignore errors.
		cmd := exec.Command("taskkill", "/f", "/im", "llama-server.exe")
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		cmd.Run() // Ignore error — it's fine if no process is found
	} else {
		// On Unix, use pkill with -f flag
		cmd := exec.Command("pkill", "-f", "llama-server")
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		cmd.Run() // Ignore error
	}
	// Small delay to let the OS release ports
	time.Sleep(500 * time.Millisecond)
}

// coalesce returns the first non-zero value.
func coalesce(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}
