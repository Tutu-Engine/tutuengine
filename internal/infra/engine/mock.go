package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── Mock Backend (for testing without CGO/llama.cpp) ───────────────────────

// MockBackend implements InferenceBackend for testing.
type MockBackend struct{}

func NewMockBackend() *MockBackend { return &MockBackend{} }

func (m *MockBackend) LoadModel(path string, opts LoadOptions) (ModelHandle, error) {
	if path == "" {
		return nil, fmt.Errorf("empty model path")
	}
	return &MockModelHandle{
		path:    path,
		memSize: 1024 * 1024 * 100, // 100MB fake
	}, nil
}

func (m *MockBackend) Close() {}

// MockModelHandle implements ModelHandle for testing.
type MockModelHandle struct {
	path    string
	memSize uint64
	closed  bool
}

func (h *MockModelHandle) Generate(ctx context.Context, prompt string, params GenerateParams) (<-chan domain.Token, error) {
	if h.closed {
		return nil, fmt.Errorf("model is closed")
	}

	ch := make(chan domain.Token, 32)
	go func() {
		defer close(ch)
		// Simulate token generation
		words := strings.Fields(fmt.Sprintf("Hello! I received your prompt: %s", prompt))
		maxTokens := params.MaxTokens
		if maxTokens == 0 {
			maxTokens = len(words)
		}

		for i, word := range words {
			if i >= maxTokens {
				break
			}
			select {
			case <-ctx.Done():
				return
			default:
				text := word
				if i < len(words)-1 && i < maxTokens-1 {
					text += " "
				}
				ch <- domain.Token{
					Text: text,
					Done: i == len(words)-1 || i == maxTokens-1,
				}
				time.Sleep(10 * time.Millisecond) // Simulate inference time
			}
		}
	}()
	return ch, nil
}

// Chat implements ModelHandle.Chat for the mock backend.
func (h *MockModelHandle) Chat(ctx context.Context, messages []ChatMessage, params GenerateParams) (<-chan domain.Token, error) {
	// Extract the last user message and delegate to Generate
	prompt := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			prompt = messages[i].Content
			break
		}
	}
	return h.Generate(ctx, prompt, params)
}

func (h *MockModelHandle) Embed(_ context.Context, input []string) ([][]float32, error) {
	if h.closed {
		return nil, fmt.Errorf("model is closed")
	}
	// Return fake 384-dimensional embeddings
	result := make([][]float32, len(input))
	for i := range input {
		vec := make([]float32, 384)
		for j := range vec {
			vec[j] = float32(j) * 0.001 * float32(i+1)
		}
		result[i] = vec
	}
	return result, nil
}

func (h *MockModelHandle) MemoryBytes() uint64 { return h.memSize }

func (h *MockModelHandle) Close() { h.closed = true }
