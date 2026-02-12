package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/engine"
)

// ─── Ollama-compatible API (/api/*) ──────────────────────────────────────────
// These endpoints mirror the Ollama REST API so that tools built for Ollama
// work with TuTu as a drop-in replacement.

// --- /api/tags (list models) ---

func (s *Server) handleOllamaTags(w http.ResponseWriter, r *http.Request) {
	models, err := s.models.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type ollamaModel struct {
		Name       string    `json:"name"`
		ModifiedAt time.Time `json:"modified_at"`
		Size       int64     `json:"size"`
		Digest     string    `json:"digest"`
	}

	out := make([]ollamaModel, len(models))
	for i, m := range models {
		out[i] = ollamaModel{
			Name:       m.Name,
			ModifiedAt: m.PulledAt,
			Size:       m.SizeBytes,
			Digest:     m.Digest,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": out,
	})
}

// --- /api/show (model details) ---

type ollamaShowRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleOllamaShow(w http.ResponseWriter, r *http.Request) {
	var req ollamaShowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	info, err := s.models.Show(req.Name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"modelfile":  "",
		"parameters": "",
		"template":   "",
		"details": map[string]interface{}{
			"format":            info.Format,
			"family":            info.Family,
			"parameter_size":    info.Parameters,
			"quantization_level": info.Quantization,
		},
	})
}

// --- /api/generate (text generation) ---

type ollamaGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream *bool  `json:"stream,omitempty"`
}

func (s *Server) handleOllamaGenerate(w http.ResponseWriter, r *http.Request) {
	var req ollamaGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	handle, err := s.pool.Acquire(req.Model, defaultLoadOpts())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer handle.Release()

	params := defaultGenParams()
	tokenCh, err := handle.Model().Generate(r.Context(), req.Prompt, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	stream := req.Stream == nil || *req.Stream

	if stream {
		s.streamOllamaGenerate(w, tokenCh, req.Model)
	} else {
		s.nonStreamOllamaGenerate(w, tokenCh, req.Model)
	}
}

func (s *Server) streamOllamaGenerate(w http.ResponseWriter, tokenCh <-chan domain.Token, model string) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	enc := json.NewEncoder(w)
	for tok := range tokenCh {
		enc.Encode(map[string]interface{}{
			"model":      model,
			"created_at": time.Now().Format(time.RFC3339Nano),
			"response":   tok.Text,
			"done":       false,
		})
		if flusher != nil {
			flusher.Flush()
		}
	}

	// Final
	enc.Encode(map[string]interface{}{
		"model":      model,
		"created_at": time.Now().Format(time.RFC3339Nano),
		"response":   "",
		"done":       true,
	})
	if flusher != nil {
		flusher.Flush()
	}
}

func (s *Server) nonStreamOllamaGenerate(w http.ResponseWriter, tokenCh <-chan domain.Token, model string) {
	var response string
	for tok := range tokenCh {
		response += tok.Text
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"model":      model,
		"created_at": time.Now().Format(time.RFC3339Nano),
		"response":   response,
		"done":       true,
	})
}

// --- /api/chat (chat generation) ---

type ollamaChatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   *bool         `json:"stream,omitempty"`
}

func (s *Server) handleOllamaChat(w http.ResponseWriter, r *http.Request) {
	var req ollamaChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	handle, err := s.pool.Acquire(req.Model, defaultLoadOpts())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer handle.Release()

	chatMsgs := make([]engine.ChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		chatMsgs[i] = engine.ChatMessage{Role: m.Role, Content: m.Content}
	}
	params := defaultGenParams()
	tokenCh, err := handle.Model().Chat(r.Context(), chatMsgs, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	stream := req.Stream == nil || *req.Stream

	if stream {
		s.streamOllamaChat(w, tokenCh, req.Model)
	} else {
		s.nonStreamOllamaChat(w, tokenCh, req.Model)
	}
}

func (s *Server) streamOllamaChat(w http.ResponseWriter, tokenCh <-chan domain.Token, model string) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	enc := json.NewEncoder(w)
	for tok := range tokenCh {
		enc.Encode(map[string]interface{}{
			"model":      model,
			"created_at": time.Now().Format(time.RFC3339Nano),
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": tok.Text,
			},
			"done": false,
		})
		if flusher != nil {
			flusher.Flush()
		}
	}

	enc.Encode(map[string]interface{}{
		"model":      model,
		"created_at": time.Now().Format(time.RFC3339Nano),
		"message": map[string]interface{}{
			"role":    "assistant",
			"content": "",
		},
		"done": true,
	})
	if flusher != nil {
		flusher.Flush()
	}
}

func (s *Server) nonStreamOllamaChat(w http.ResponseWriter, tokenCh <-chan domain.Token, model string) {
	var content string
	for tok := range tokenCh {
		content += tok.Text
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"model":      model,
		"created_at": time.Now().Format(time.RFC3339Nano),
		"message": map[string]interface{}{
			"role":    "assistant",
			"content": content,
		},
		"done": true,
	})
}

// --- /api/pull ---

type ollamaPullRequest struct {
	Name   string `json:"name"`
	Stream *bool  `json:"stream,omitempty"`
}

func (s *Server) handleOllamaPull(w http.ResponseWriter, r *http.Request) {
	var req ollamaPullRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	err := s.models.Pull(req.Name, func(status string, pct float64) {
		// For non-streaming, we just wait
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "success",
	})
}

// --- /api/delete ---

type ollamaDeleteRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleOllamaDelete(w http.ResponseWriter, r *http.Request) {
	var req ollamaDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.models.Remove(req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
}

// --- /api/ps (running models) ---

func (s *Server) handleOllamaPs(w http.ResponseWriter, r *http.Request) {
	loaded := s.pool.LoadedModels()

	type ollamaPs struct {
		Name      string    `json:"name"`
		Size      int64     `json:"size"`
		Processor string    `json:"processor"`
		ExpiresAt time.Time `json:"expires_at"`
	}

	models := make([]ollamaPs, len(loaded))
	for i, m := range loaded {
		models[i] = ollamaPs{
			Name:      m.Name,
			Size:      m.SizeBytes,
			Processor: m.Processor,
			ExpiresAt: m.ExpiresAt,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": models,
	})
}


