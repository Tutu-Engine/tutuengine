package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/tutu-network/tutu/internal/infra/engine"
)

// ─── OpenAI-compatible API (/v1/*) ──────────────────────────────────────────
// These endpoints mimic the OpenAI API format so that any tool built for
// OpenAI or compatible providers can talk to TuTu out of the box.

// --- /v1/models ---

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.models.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	data := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		data = append(data, modelToOpenAI(m))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data":   data,
	})
}

// --- /v1/chat/completions ---

// chatRequest is the OpenAI chat completions request body.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature *float32      `json:"temperature,omitempty"`
	TopP        *float32      `json:"top_p,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream"`
	Stop        []string      `json:"stop,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	// Acquire model from pool
	handle, err := s.pool.Acquire(req.Model, defaultLoadOpts())
	if err != nil {
		writeError(w, http.StatusBadRequest, "model error: "+err.Error())
		return
	}
	defer handle.Release()

	// Build chat messages for the engine
	chatMsgs := make([]engine.ChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		chatMsgs[i] = engine.ChatMessage{Role: m.Role, Content: m.Content}
	}

	// Set generation params
	params := defaultGenParams()
	if req.Temperature != nil {
		params.Temperature = *req.Temperature
	}
	if req.TopP != nil {
		params.TopP = *req.TopP
	}
	if req.MaxTokens != nil {
		params.MaxTokens = *req.MaxTokens
	}
	if len(req.Stop) > 0 {
		params.Stop = req.Stop
	}

	completionID := "chatcmpl-" + uuid.New().String()[:8]

	if req.Stream {
		s.streamChatResponse(w, r.Context(), handle, chatMsgs, params, req.Model, completionID)
	} else {
		s.nonStreamChatResponse(w, r.Context(), handle, chatMsgs, params, req.Model, completionID)
	}
}

func (s *Server) nonStreamChatResponse(w http.ResponseWriter, ctx context.Context, handle *engine.PoolHandle, messages []engine.ChatMessage, params engine.GenerateParams, model, completionID string) {
	tokenCh, err := handle.Model().Chat(ctx, messages, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Collect all tokens
	var content string
	// Rough estimate of prompt tokens based on message content
	promptChars := 0
	for _, m := range messages {
		promptChars += len(m.Content)
	}
	promptTokens := promptChars / 4
	completionTokens := 0

	for tok := range tokenCh {
		content += tok.Text
		completionTokens++
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":      completionID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		},
	})
}

func (s *Server) streamChatResponse(w http.ResponseWriter, ctx context.Context, handle *engine.PoolHandle, messages []engine.ChatMessage, params engine.GenerateParams, model, completionID string) {
	tokenCh, err := handle.Model().Chat(ctx, messages, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	writer := bufio.NewWriter(w)

	for tok := range tokenCh {
		chunk := map[string]interface{}{
			"id":      completionID,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"delta": map[string]interface{}{
						"content": tok.Text,
					},
					"finish_reason": nil,
				},
			},
		}

		data, _ := json.Marshal(chunk)
		fmt.Fprintf(writer, "data: %s\n\n", data)
		writer.Flush()
		flusher.Flush()
	}

	// Send final chunk with finish_reason
	finalChunk := map[string]interface{}{
		"id":      completionID,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": "stop",
			},
		},
	}

	data, _ := json.Marshal(finalChunk)
	fmt.Fprintf(writer, "data: %s\n\n", data)
	fmt.Fprintf(writer, "data: [DONE]\n\n")
	writer.Flush()
	flusher.Flush()
}

// --- /v1/embeddings ---

type embeddingRequest struct {
	Model string      `json:"model"`
	Input interface{} `json:"input"` // string or []string
}

func (s *Server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	var req embeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	// Normalize input to []string
	var inputs []string
	switch v := req.Input.(type) {
	case string:
		inputs = []string{v}
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				inputs = append(inputs, str)
			}
		}
	default:
		writeError(w, http.StatusBadRequest, "input must be a string or array of strings")
		return
	}

	handle, err := s.pool.Acquire(req.Model, defaultLoadOpts())
	if err != nil {
		writeError(w, http.StatusBadRequest, "model error: "+err.Error())
		return
	}
	defer handle.Release()

	embeddings, err := handle.Model().Embed(r.Context(), inputs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	data := make([]map[string]interface{}, len(embeddings))
	for i, emb := range embeddings {
		data[i] = map[string]interface{}{
			"object":    "embedding",
			"embedding": emb,
			"index":     i,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data":   data,
		"model":  req.Model,
		"usage": map[string]interface{}{
			"prompt_tokens": len(inputs),
			"total_tokens":  len(inputs),
		},
	})
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// buildPrompt concatenates chat messages into a single prompt string.
// In a real implementation, this would use chat templates per model.
func buildPrompt(messages []chatMessage) string {
	var prompt string
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			prompt += fmt.Sprintf("[SYSTEM] %s\n", msg.Content)
		case "user":
			prompt += fmt.Sprintf("[USER] %s\n", msg.Content)
		case "assistant":
			prompt += fmt.Sprintf("[ASSISTANT] %s\n", msg.Content)
		default:
			prompt += fmt.Sprintf("[%s] %s\n", msg.Role, msg.Content)
		}
	}
	prompt += "[ASSISTANT] "
	return prompt
}
