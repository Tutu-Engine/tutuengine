package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/google/uuid"
)

// ─── Streamable HTTP Transport ──────────────────────────────────────────────
// Architecture Part XII: MCP 2025-03-26 Streamable HTTP transport.
//
// POST /mcp   → JSON-RPC 2.0 request/response
// GET  /mcp   → SSE stream for server-initiated notifications
// DELETE /mcp → Close session
//
// Sessions are tracked via Mcp-Session-Id header.
// The transport is stateless per request — each POST is independent.

// Transport provides the HTTP handlers for the MCP protocol.
type Transport struct {
	gateway  *Gateway
	mu       sync.RWMutex
	sessions map[string]*session
}

// session tracks a connected MCP client session.
type session struct {
	ID        string
	ClientName string
	// SSE channel for server-initiated notifications
	notify chan []byte
	done   chan struct{}
}

// NewTransport creates a new Streamable HTTP transport.
func NewTransport(gateway *Gateway) *Transport {
	return &Transport{
		gateway:  gateway,
		sessions: make(map[string]*session),
	}
}

// ServeHTTP implements http.Handler — the single MCP endpoint.
func (t *Transport) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		t.handlePost(w, r)
	case http.MethodGet:
		t.handleSSE(w, r)
	case http.MethodDelete:
		t.handleDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePost processes a JSON-RPC 2.0 request.
func (t *Transport) handlePost(w http.ResponseWriter, r *http.Request) {
	// Read request body
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if len(body) == 0 {
		http.Error(w, "Empty request body", http.StatusBadRequest)
		return
	}

	// Dispatch to gateway
	resp := t.gateway.HandleRequest(body)

	// Notifications return no response — 202 Accepted
	if resp == nil {
		// Ensure session header on notifications too
		sessionID := r.Header.Get("Mcp-Session-Id")
		if sessionID == "" {
			sessionID = uuid.New().String()
		}
		w.Header().Set("Mcp-Session-Id", sessionID)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Check if this is an initialize response — assign session
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Track session on initialize
	if isInitializeResponse(body) {
		t.mu.Lock()
		t.sessions[sessionID] = &session{
			ID:     sessionID,
			notify: make(chan []byte, 32),
			done:   make(chan struct{}),
		}
		t.mu.Unlock()
		log.Printf("[mcp/transport] new session: %s", sessionID)
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Mcp-Session-Id", sessionID)

	data, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "Failed to marshal response", http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

// handleSSE opens a Server-Sent Events stream for server-initiated notifications.
func (t *Transport) handleSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "Mcp-Session-Id required", http.StatusBadRequest)
		return
	}

	t.mu.RLock()
	sess, ok := t.sessions[sessionID]
	t.mu.RUnlock()
	if !ok {
		http.Error(w, "Unknown session", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Mcp-Session-Id", sessionID)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-sess.done:
			return
		case msg := <-sess.notify:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

// handleDelete closes a client session.
func (t *Transport) handleDelete(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "Mcp-Session-Id required", http.StatusBadRequest)
		return
	}

	t.mu.Lock()
	sess, ok := t.sessions[sessionID]
	if ok {
		close(sess.done)
		delete(t.sessions, sessionID)
	}
	t.mu.Unlock()

	if !ok {
		http.Error(w, "Unknown session", http.StatusNotFound)
		return
	}

	log.Printf("[mcp/transport] session closed: %s", sessionID)
	w.WriteHeader(http.StatusOK)
}

// Notify sends a server-initiated notification to a specific session.
func (t *Transport) Notify(sessionID string, notification Notification) error {
	t.mu.RLock()
	sess, ok := t.sessions[sessionID]
	t.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	data, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	select {
	case sess.notify <- data:
		return nil
	default:
		return fmt.Errorf("notification buffer full for session %s", sessionID)
	}
}

// SessionCount returns the number of active sessions.
func (t *Transport) SessionCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.sessions)
}

// isInitializeResponse checks if the request was an initialize call.
func isInitializeResponse(body []byte) bool {
	var req struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}
	return req.Method == "initialize"
}
