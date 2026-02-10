package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── Test Helpers ───────────────────────────────────────────────────────────

func newTestGateway(t *testing.T) *Gateway {
	t.Helper()
	sla := NewSLAEngine()
	meter := NewMeter(sla)
	return NewGateway(sla, meter)
}

func rpcRequest(method string, params any) []byte {
	p, _ := json.Marshal(params)
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      1,
		Method:  method,
		Params:  p,
	}
	data, _ := json.Marshal(req)
	return data
}

func rpcRequestRaw(method string, rawParams json.RawMessage) []byte {
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      1,
		Method:  method,
		Params:  rawParams,
	}
	data, _ := json.Marshal(req)
	return data
}

// ─── JSON-RPC 2.0 Codec Tests ──────────────────────────────────────────────

func TestParseRequest_Valid(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	req, errResp := ParseRequest(raw)
	if errResp != nil {
		t.Fatalf("unexpected error: %v", errResp.Error)
	}
	if req.Method != "ping" {
		t.Errorf("method = %q, want ping", req.Method)
	}
}

func TestParseRequest_InvalidJSON(t *testing.T) {
	raw := []byte(`{broken json}`)
	_, errResp := ParseRequest(raw)
	if errResp == nil {
		t.Fatal("expected parse error")
	}
	if errResp.Error.Code != CodeParseError {
		t.Errorf("error code = %d, want %d", errResp.Error.Code, CodeParseError)
	}
}

func TestParseRequest_WrongVersion(t *testing.T) {
	raw := []byte(`{"jsonrpc":"1.0","id":1,"method":"ping"}`)
	_, errResp := ParseRequest(raw)
	if errResp == nil {
		t.Fatal("expected invalid request error")
	}
	if errResp.Error.Code != CodeInvalidRequest {
		t.Errorf("error code = %d, want %d", errResp.Error.Code, CodeInvalidRequest)
	}
}

func TestParseRequest_MissingMethod(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":1}`)
	_, errResp := ParseRequest(raw)
	if errResp == nil {
		t.Fatal("expected invalid request error")
	}
}

func TestNewResult(t *testing.T) {
	resp, err := NewResult(1, map[string]string{"hello": "world"})
	if err != nil {
		t.Fatalf("NewResult error: %v", err)
	}
	if resp.JSONRPC != JSONRPCVersion {
		t.Errorf("jsonrpc = %q, want %q", resp.JSONRPC, JSONRPCVersion)
	}
	if resp.Error != nil {
		t.Error("expected no error")
	}
}

func TestRPCError_Error(t *testing.T) {
	e := &RPCError{Code: -32601, Message: "Method not found"}
	s := e.Error()
	if !strings.Contains(s, "-32601") || !strings.Contains(s, "Method not found") {
		t.Errorf("Error() = %q, expected code and message", s)
	}
}

func TestErrorConstructors(t *testing.T) {
	tests := []struct {
		name string
		resp Response
		code int
	}{
		{"ParseError", NewParseError(1), CodeParseError},
		{"InvalidRequest", NewInvalidRequest(1), CodeInvalidRequest},
		{"MethodNotFound", NewMethodNotFound(1, "foo"), CodeMethodNotFound},
		{"InvalidParams", NewInvalidParams(1, "bad"), CodeInvalidParams},
		{"InternalError", NewInternalError(1, "oops"), CodeInternalError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.resp.Error == nil {
				t.Fatal("expected error")
			}
			if tt.resp.Error.Code != tt.code {
				t.Errorf("code = %d, want %d", tt.resp.Error.Code, tt.code)
			}
		})
	}
}

// ─── SLA Engine Tests ───────────────────────────────────────────────────────

func TestSLAEngine_AllTiers(t *testing.T) {
	sla := NewSLAEngine()
	tiers := sla.AllTiers()
	if len(tiers) != 4 {
		t.Fatalf("expected 4 tiers, got %d", len(tiers))
	}
	// Verify priority order (highest first)
	if tiers[0].Priority <= tiers[1].Priority {
		t.Error("tiers should be sorted by priority descending")
	}
}

func TestSLAEngine_ConfigFor_Realtime(t *testing.T) {
	sla := NewSLAEngine()
	cfg := sla.ConfigFor(domain.SLARealtime)
	if cfg.MaxLatencyP99 != 200*time.Millisecond {
		t.Errorf("realtime latency = %v, want 200ms", cfg.MaxLatencyP99)
	}
	if cfg.PricePerMTokens != 2.00 {
		t.Errorf("realtime price = %v, want 2.00", cfg.PricePerMTokens)
	}
	if cfg.Priority != 255 {
		t.Errorf("realtime priority = %d, want 255", cfg.Priority)
	}
}

func TestSLAEngine_ConfigFor_Standard(t *testing.T) {
	sla := NewSLAEngine()
	cfg := sla.ConfigFor(domain.SLAStandard)
	if cfg.MaxLatencyP99 != 2*time.Second {
		t.Errorf("standard latency = %v, want 2s", cfg.MaxLatencyP99)
	}
	if cfg.Priority != 128 {
		t.Errorf("standard priority = %d, want 128", cfg.Priority)
	}
}

func TestSLAEngine_ConfigFor_Batch(t *testing.T) {
	sla := NewSLAEngine()
	cfg := sla.ConfigFor(domain.SLABatch)
	if cfg.PricePerMTokens != 0.10 {
		t.Errorf("batch price = %v, want 0.10", cfg.PricePerMTokens)
	}
}

func TestSLAEngine_ConfigFor_Spot(t *testing.T) {
	sla := NewSLAEngine()
	cfg := sla.ConfigFor(domain.SLASpot)
	if cfg.PricePerMTokens != 0.02 {
		t.Errorf("spot price = %v, want 0.02", cfg.PricePerMTokens)
	}
	if cfg.Priority != 1 {
		t.Errorf("spot priority = %d, want 1", cfg.Priority)
	}
}

func TestSLAEngine_ConfigFor_Unknown(t *testing.T) {
	sla := NewSLAEngine()
	cfg := sla.ConfigFor(domain.SLATier("nonexistent"))
	// Falls back to spot
	if cfg.Tier != domain.SLASpot {
		t.Errorf("unknown tier should fall back to spot, got %s", cfg.Tier)
	}
}

func TestSLAEngine_PriorityFor(t *testing.T) {
	sla := NewSLAEngine()
	if sla.PriorityFor(domain.SLARealtime) != 255 {
		t.Error("realtime priority should be 255")
	}
	if sla.PriorityFor(domain.SLASpot) != 1 {
		t.Error("spot priority should be 1")
	}
}

func TestSLAEngine_CostMicro(t *testing.T) {
	sla := NewSLAEngine()
	// Realtime: $2.00/M tokens. 1000 tokens = $0.002 = 2000 microdollars
	cost := sla.CostMicro(domain.SLARealtime, 500, 500)
	if cost != 2000 {
		t.Errorf("realtime cost for 1000 tokens = %d microdollars, want 2000", cost)
	}
}

func TestSLAEngine_CostMicro_Spot(t *testing.T) {
	sla := NewSLAEngine()
	// Spot: $0.02/M tokens. 1000 tokens = $0.00002 = 20 microdollars
	cost := sla.CostMicro(domain.SLASpot, 500, 500)
	if cost != 20 {
		t.Errorf("spot cost for 1000 tokens = %d microdollars, want 20", cost)
	}
}

// ─── Meter Tests ────────────────────────────────────────────────────────────

func TestMeter_Record(t *testing.T) {
	sla := NewSLAEngine()
	m := NewMeter(sla)

	rec := m.Record("client-1", "tutu_inference", "llama-7b", 100, 50, 42, domain.SLAStandard)
	if rec.ClientID != "client-1" {
		t.Errorf("client = %q, want client-1", rec.ClientID)
	}
	if rec.CostMicro != sla.CostMicro(domain.SLAStandard, 100, 50) {
		t.Error("cost not calculated correctly")
	}
	if m.TotalRecords() != 1 {
		t.Errorf("total records = %d, want 1", m.TotalRecords())
	}
}

func TestMeter_ClientSummary(t *testing.T) {
	sla := NewSLAEngine()
	m := NewMeter(sla)

	m.Record("client-1", "tutu_inference", "llama-7b", 100, 50, 42, domain.SLAStandard)
	m.Record("client-1", "tutu_embed", "embed-v2", 200, 0, 15, domain.SLAStandard)
	m.Record("client-2", "tutu_inference", "llama-7b", 300, 100, 80, domain.SLARealtime)

	s := m.ClientSummary("client-1")
	if s.TotalCalls != 2 {
		t.Errorf("calls = %d, want 2", s.TotalCalls)
	}
	if s.TotalInput != 300 {
		t.Errorf("input = %d, want 300", s.TotalInput)
	}
	if s.TotalOutput != 50 {
		t.Errorf("output = %d, want 50", s.TotalOutput)
	}
}

func TestMeter_ClientSummary_Unknown(t *testing.T) {
	sla := NewSLAEngine()
	m := NewMeter(sla)

	s := m.ClientSummary("nonexistent")
	if s.TotalCalls != 0 {
		t.Error("unknown client should have 0 calls")
	}
}

func TestMeter_RecentRecords(t *testing.T) {
	sla := NewSLAEngine()
	m := NewMeter(sla)

	m.Record("c1", "tutu_inference", "m1", 10, 5, 1, domain.SLASpot)
	m.Record("c1", "tutu_embed", "m2", 20, 0, 2, domain.SLASpot)
	m.Record("c1", "tutu_inference", "m3", 30, 10, 3, domain.SLASpot)

	recent := m.RecentRecords(2)
	if len(recent) != 2 {
		t.Fatalf("recent len = %d, want 2", len(recent))
	}
	// Most recent first
	if recent[0].Model != "m3" {
		t.Errorf("most recent model = %q, want m3", recent[0].Model)
	}
	if recent[1].Model != "m2" {
		t.Errorf("second recent model = %q, want m2", recent[1].Model)
	}
}

func TestMeter_RecentRecords_MoreThanAvailable(t *testing.T) {
	sla := NewSLAEngine()
	m := NewMeter(sla)

	m.Record("c1", "tutu_inference", "m1", 10, 5, 1, domain.SLASpot)
	recent := m.RecentRecords(100)
	if len(recent) != 1 {
		t.Errorf("recent len = %d, want 1", len(recent))
	}
}

func TestMeter_Reset(t *testing.T) {
	sla := NewSLAEngine()
	m := NewMeter(sla)

	m.Record("c1", "tutu_inference", "m1", 10, 5, 1, domain.SLASpot)
	m.Reset()

	if m.TotalRecords() != 0 {
		t.Error("records should be empty after reset")
	}
	s := m.ClientSummary("c1")
	if s.TotalCalls != 0 {
		t.Error("client summary should be empty after reset")
	}
}

// ─── Gateway Tests ──────────────────────────────────────────────────────────

func TestGateway_Initialize(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"clientInfo":      map[string]string{"name": "test-client", "version": "1.0"},
	})

	resp := gw.HandleRequest(raw)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result initializeResult
	json.Unmarshal(resp.Result, &result)
	if result.ProtocolVersion != MCPProtocolVersion {
		t.Errorf("protocol = %q, want %q", result.ProtocolVersion, MCPProtocolVersion)
	}
	if result.ServerInfo.Name != ServerName {
		t.Errorf("server = %q, want %q", result.ServerInfo.Name, ServerName)
	}
	if result.Capabilities.Tools == nil {
		t.Error("expected tools capability")
	}
	if result.Capabilities.Resources == nil {
		t.Error("expected resources capability")
	}
}

func TestGateway_Ping(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("ping", nil)

	resp := gw.HandleRequest(raw)
	if resp == nil {
		t.Fatal("expected response to ping")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

func TestGateway_MethodNotFound(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("nonexistent/method", nil)

	resp := gw.HandleRequest(raw)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("code = %d, want %d", resp.Error.Code, CodeMethodNotFound)
	}
}

func TestGateway_ToolsList(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("tools/list", nil)

	resp := gw.HandleRequest(raw)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result toolsListResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(result.Tools))
	}

	names := make(map[string]bool)
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	for _, expected := range []string{"tutu_inference", "tutu_embed", "tutu_batch_process", "tutu_fine_tune"} {
		if !names[expected] {
			t.Errorf("missing tool: %s", expected)
		}
	}
}

func TestGateway_ResourcesList(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("resources/list", nil)

	resp := gw.HandleRequest(raw)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result resourcesListResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Resources) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(result.Resources))
	}
}

func TestGateway_ToolsCall_Inference(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("tools/call", toolsCallParams{
		Name: "tutu_inference",
		Arguments: mustMarshal(domain.InferenceParams{
			Model:  "llama-3.2-7b",
			Prompt: "Hello, world!",
			Stream: false,
		}),
	})

	resp := gw.HandleRequest(raw)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result toolsCallResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Content) == 0 {
		t.Fatal("expected content in result")
	}
	if result.Content[0].Type != "text" {
		t.Errorf("content type = %q, want text", result.Content[0].Type)
	}
	if !strings.Contains(result.Content[0].Text, "llama-3.2-7b") {
		t.Error("response should mention the model")
	}
}

func TestGateway_ToolsCall_Inference_MissingModel(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("tools/call", toolsCallParams{
		Name:      "tutu_inference",
		Arguments: mustMarshal(domain.InferenceParams{Prompt: "hello"}),
	})

	resp := gw.HandleRequest(raw)
	if resp.Error == nil {
		t.Fatal("expected error for missing model")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

func TestGateway_ToolsCall_Inference_MissingPrompt(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("tools/call", toolsCallParams{
		Name:      "tutu_inference",
		Arguments: mustMarshal(domain.InferenceParams{Model: "llama-7b"}),
	})

	resp := gw.HandleRequest(raw)
	if resp.Error == nil {
		t.Fatal("expected error for missing prompt")
	}
}

func TestGateway_ToolsCall_Embed(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("tools/call", toolsCallParams{
		Name: "tutu_embed",
		Arguments: mustMarshal(domain.EmbedParams{
			Model:  "embed-v2",
			Inputs: []string{"hello world", "test input"},
		}),
	})

	resp := gw.HandleRequest(raw)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

func TestGateway_ToolsCall_Embed_EmptyInputs(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("tools/call", toolsCallParams{
		Name: "tutu_embed",
		Arguments: mustMarshal(domain.EmbedParams{
			Model:  "embed-v2",
			Inputs: []string{},
		}),
	})

	resp := gw.HandleRequest(raw)
	if resp.Error == nil {
		t.Fatal("expected error for empty inputs")
	}
}

func TestGateway_ToolsCall_Batch(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("tools/call", toolsCallParams{
		Name: "tutu_batch_process",
		Arguments: mustMarshal(domain.BatchParams{
			Model:   "llama-7b",
			Prompts: []string{"prompt1", "prompt2"},
			Tier:    domain.SLABatch,
		}),
	})

	resp := gw.HandleRequest(raw)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

func TestGateway_ToolsCall_FineTune(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("tools/call", toolsCallParams{
		Name: "tutu_fine_tune",
		Arguments: mustMarshal(domain.FineTuneParams{
			BaseModel:  "llama-7b",
			DatasetURI: "s3://my-bucket/data.jsonl",
			Epochs:     5,
			LoRA:       true,
		}),
	})

	resp := gw.HandleRequest(raw)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result toolsCallResult
	json.Unmarshal(resp.Result, &result)
	if !strings.Contains(result.Content[0].Text, "lora=true") {
		t.Error("response should mention LoRA")
	}
}

func TestGateway_ToolsCall_UnknownTool(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("tools/call", toolsCallParams{
		Name:      "unknown_tool",
		Arguments: mustMarshal(map[string]string{}),
	})

	resp := gw.HandleRequest(raw)
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestGateway_ResourcesRead_Capacity(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("resources/read", resourcesReadParams{URI: "tutu://capacity"})

	resp := gw.HandleRequest(raw)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result resourcesReadResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Contents))
	}
	if result.Contents[0].URI != "tutu://capacity" {
		t.Errorf("URI = %q, want tutu://capacity", result.Contents[0].URI)
	}
	if result.Contents[0].MimeType != "application/json" {
		t.Errorf("mimeType = %q, want application/json", result.Contents[0].MimeType)
	}
}

func TestGateway_ResourcesRead_Models(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("resources/read", resourcesReadParams{URI: "tutu://models"})

	resp := gw.HandleRequest(raw)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result resourcesReadResult
	json.Unmarshal(resp.Result, &result)
	if !strings.Contains(result.Contents[0].Text, "llama-3.2-7b") {
		t.Error("models should include llama-3.2-7b")
	}
}

func TestGateway_ResourcesRead_UnknownURI(t *testing.T) {
	gw := newTestGateway(t)
	raw := rpcRequest("resources/read", resourcesReadParams{URI: "tutu://unknown"})

	resp := gw.HandleRequest(raw)
	if resp.Error == nil {
		t.Fatal("expected error for unknown resource")
	}
}

func TestGateway_Notification_NoResponse(t *testing.T) {
	gw := newTestGateway(t)
	// Notification = no id field
	raw := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	resp := gw.HandleRequest(raw)
	if resp != nil {
		t.Error("notifications should return nil response")
	}
}

func TestGateway_Inference_MetersUsage(t *testing.T) {
	sla := NewSLAEngine()
	meter := NewMeter(sla)
	gw := NewGateway(sla, meter)

	raw := rpcRequest("tools/call", toolsCallParams{
		Name: "tutu_inference",
		Arguments: mustMarshal(domain.InferenceParams{
			Model:  "llama-7b",
			Prompt: "Hello, world! This is a test prompt.",
		}),
	})
	gw.HandleRequest(raw)

	if meter.TotalRecords() != 1 {
		t.Fatalf("expected 1 metered record, got %d", meter.TotalRecords())
	}
	recent := meter.RecentRecords(1)
	if recent[0].Tool != "tutu_inference" {
		t.Errorf("tool = %q, want tutu_inference", recent[0].Tool)
	}
}

// ─── Transport Tests ────────────────────────────────────────────────────────

func TestTransport_Post_Initialize(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)

	body := rpcRequest("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"clientInfo":      map[string]string{"name": "test"},
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	tr.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	sessionID := w.Header().Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Error("expected Mcp-Session-Id header")
	}

	// Session should be tracked
	if tr.SessionCount() != 1 {
		t.Errorf("sessions = %d, want 1", tr.SessionCount())
	}
}

func TestTransport_Post_ToolsCall(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)

	body := rpcRequest("tools/call", toolsCallParams{
		Name: "tutu_inference",
		Arguments: mustMarshal(domain.InferenceParams{
			Model:  "llama-7b",
			Prompt: "test",
		}),
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	tr.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

func TestTransport_Post_EmptyBody(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(""))
	w := httptest.NewRecorder()
	tr.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestTransport_Post_Notification(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)

	// Notification has no id
	body := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	tr.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", w.Code)
	}
}

func TestTransport_Delete_Session(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)

	// First, create a session via initialize
	body := rpcRequest("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"clientInfo":      map[string]string{"name": "test"},
	})
	initReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
	initW := httptest.NewRecorder()
	tr.ServeHTTP(initW, initReq)

	sessionID := initW.Header().Get("Mcp-Session-Id")

	// Delete the session
	delReq := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	delReq.Header.Set("Mcp-Session-Id", sessionID)
	delW := httptest.NewRecorder()
	tr.ServeHTTP(delW, delReq)

	if delW.Code != http.StatusOK {
		t.Errorf("delete status = %d, want 200", delW.Code)
	}
	if tr.SessionCount() != 0 {
		t.Errorf("sessions = %d, want 0 after delete", tr.SessionCount())
	}
}

func TestTransport_Delete_UnknownSession(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)

	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "nonexistent")
	w := httptest.NewRecorder()
	tr.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestTransport_Delete_NoSession(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)

	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	w := httptest.NewRecorder()
	tr.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestTransport_MethodNotAllowed(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)

	req := httptest.NewRequest(http.MethodPut, "/mcp", nil)
	w := httptest.NewRecorder()
	tr.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestTransport_SSE_NoSession(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()
	tr.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestTransport_SSE_UnknownSession(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "nonexistent")
	w := httptest.NewRecorder()
	tr.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestTransport_Notify(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)

	// Create session
	body := rpcRequest("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"clientInfo":      map[string]string{"name": "test"},
	})
	initReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
	initW := httptest.NewRecorder()
	tr.ServeHTTP(initW, initReq)
	sessionID := initW.Header().Get("Mcp-Session-Id")

	// Send notification
	notif := Notification{
		JSONRPC: JSONRPCVersion,
		Method:  "notifications/resources/updated",
	}
	err := tr.Notify(sessionID, notif)
	if err != nil {
		t.Fatalf("Notify error: %v", err)
	}
}

func TestTransport_Notify_UnknownSession(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)

	notif := Notification{JSONRPC: JSONRPCVersion, Method: "test"}
	err := tr.Notify("nonexistent", notif)
	if err == nil {
		t.Error("expected error for unknown session")
	}
}

// ─── Integration: Full MCP Flow ─────────────────────────────────────────────

func TestIntegration_FullMCPFlow(t *testing.T) {
	sla := NewSLAEngine()
	meter := NewMeter(sla)
	gw := NewGateway(sla, meter)
	tr := NewTransport(gw)

	ts := httptest.NewServer(tr)
	defer ts.Close()

	// 1. Initialize
	initBody := rpcRequest("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"clientInfo":      map[string]string{"name": "integration-test"},
	})
	initResp, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(string(initBody)))
	if err != nil {
		t.Fatalf("init request failed: %v", err)
	}
	if initResp.StatusCode != 200 {
		t.Fatalf("init status = %d", initResp.StatusCode)
	}
	sessionID := initResp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("no session ID")
	}
	initResp.Body.Close()

	// 2. List tools
	toolsBody := rpcRequest("tools/list", nil)
	toolsResp, _ := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(string(toolsBody)))
	var toolsResult struct {
		Result struct {
			Tools []domain.MCPTool `json:"tools"`
		} `json:"result"`
	}
	respBody, _ := io.ReadAll(toolsResp.Body)
	json.Unmarshal(respBody, &toolsResult)
	toolsResp.Body.Close()
	if len(toolsResult.Result.Tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(toolsResult.Result.Tools))
	}

	// 3. Call inference tool
	callBody := rpcRequest("tools/call", toolsCallParams{
		Name: "tutu_inference",
		Arguments: mustMarshal(domain.InferenceParams{
			Model:    "llama-3.2-70b",
			Prompt:   "What is the meaning of life?",
			Priority: domain.SLARealtime,
		}),
	})
	callResp, _ := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(string(callBody)))
	if callResp.StatusCode != 200 {
		t.Fatalf("call status = %d", callResp.StatusCode)
	}
	callResp.Body.Close()

	// 4. Read capacity resource
	capBody := rpcRequest("resources/read", resourcesReadParams{URI: "tutu://capacity"})
	capResp, _ := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(string(capBody)))
	if capResp.StatusCode != 200 {
		t.Fatalf("capacity status = %d", capResp.StatusCode)
	}
	capResp.Body.Close()

	// 5. Verify metering
	if meter.TotalRecords() != 1 {
		t.Errorf("expected 1 metered record, got %d", meter.TotalRecords())
	}

	// 6. Delete session
	delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/mcp", nil)
	delReq.Header.Set("Mcp-Session-Id", sessionID)
	delResp, _ := http.DefaultClient.Do(delReq)
	if delResp.StatusCode != 200 {
		t.Fatalf("delete status = %d", delResp.StatusCode)
	}
	delResp.Body.Close()

	if tr.SessionCount() != 0 {
		t.Error("session should be deleted")
	}
}

// ─── Tool Input Schema Tests ────────────────────────────────────────────────

func TestToolSchemas_HaveRequiredFields(t *testing.T) {
	gw := newTestGateway(t)
	for _, tool := range gw.tools {
		t.Run(tool.Name, func(t *testing.T) {
			if tool.InputSchema.Type != "object" {
				t.Errorf("schema type = %q, want object", tool.InputSchema.Type)
			}
			if len(tool.InputSchema.Required) == 0 {
				t.Error("expected required fields")
			}
			for _, req := range tool.InputSchema.Required {
				if _, ok := tool.InputSchema.Properties[req]; !ok {
					t.Errorf("required field %q not in properties", req)
				}
			}
		})
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func mustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustMarshal: %v", err))
	}
	return data
}
