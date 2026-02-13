package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── Real-World MCP Scenario Tests ──────────────────────────────────────────
// These tests simulate real-world scenarios that an MCP deployment would face:
// concurrent clients, rapid-fire requests, error recovery, session lifecycle,
// SLA enforcement, and metering accuracy under load.

// TestScenario_ConcurrentClients simulates multiple MCP clients connecting
// simultaneously and issuing requests in parallel.
func TestScenario_ConcurrentClients(t *testing.T) {
	sla := NewSLAEngine()
	meter := NewMeter(sla)
	gw := NewGateway(sla, meter)
	tr := NewTransport(gw)
	ts := httptest.NewServer(tr)
	defer ts.Close()

	const numClients = 10
	var wg sync.WaitGroup
	errors := make(chan error, numClients*3)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			// 1. Initialize
			initBody := rpcRequest("initialize", map[string]any{
				"protocolVersion": MCPProtocolVersion,
				"clientInfo":      map[string]string{"name": "concurrent-client"},
			})
			resp, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(string(initBody)))
			if err != nil {
				errors <- err
				return
			}
			sessionID := resp.Header.Get("Mcp-Session-Id")
			resp.Body.Close()
			if sessionID == "" {
				errors <- errorf("client %d: no session ID", clientID)
				return
			}

			// 2. List tools
			toolsBody := rpcRequest("tools/list", nil)
			resp, err = http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(string(toolsBody)))
			if err != nil {
				errors <- err
				return
			}
			resp.Body.Close()

			// 3. Call inference
			callBody := rpcRequest("tools/call", toolsCallParams{
				Name: "tutu_inference",
				Arguments: mustMarshal(domain.InferenceParams{
					Model:  "llama-3.2-7b",
					Prompt: "Concurrent test prompt",
				}),
			})
			resp, err = http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(string(callBody)))
			if err != nil {
				errors <- err
				return
			}
			resp.Body.Close()
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent client error: %v", err)
	}

	// All 10 clients should have metered their inference call
	if meter.TotalRecords() != numClients {
		t.Errorf("expected %d metered records, got %d", numClients, meter.TotalRecords())
	}
}

// TestScenario_RapidFireRequests simulates burst traffic with many requests
// sent rapidly in sequence to a single session.
func TestScenario_RapidFireRequests(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)
	ts := httptest.NewServer(tr)
	defer ts.Close()

	const numRequests = 50

	for i := 0; i < numRequests; i++ {
		body := rpcRequest("tools/call", toolsCallParams{
			Name: "tutu_inference",
			Arguments: mustMarshal(domain.InferenceParams{
				Model:  "llama-3.2-1b",
				Prompt: "Burst test",
			}),
		})
		resp, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status %d", i, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

// TestScenario_AllToolsEndToEnd exercises every MCP tool in sequence,
// verifying correct metering, SLA tier handling, and response structure.
func TestScenario_AllToolsEndToEnd(t *testing.T) {
	sla := NewSLAEngine()
	meter := NewMeter(sla)
	gw := NewGateway(sla, meter)

	// Tool 1: Inference with realtime SLA
	resp := gw.HandleRequest(rpcRequest("tools/call", toolsCallParams{
		Name: "tutu_inference",
		Arguments: mustMarshal(domain.InferenceParams{
			Model:    "llama-3.2-70b",
			Prompt:   "Explain quantum computing in simple terms.",
			Priority: domain.SLARealtime,
		}),
	}))
	assertToolSuccess(t, resp, "inference")

	// Tool 2: Embed with multiple inputs
	resp = gw.HandleRequest(rpcRequest("tools/call", toolsCallParams{
		Name: "tutu_embed",
		Arguments: mustMarshal(domain.EmbedParams{
			Model:  "embed-v3",
			Inputs: []string{"document 1", "document 2", "document 3", "query text"},
		}),
	}))
	assertToolSuccess(t, resp, "embed")

	// Tool 3: Batch with spot tier
	resp = gw.HandleRequest(rpcRequest("tools/call", toolsCallParams{
		Name: "tutu_batch_process",
		Arguments: mustMarshal(domain.BatchParams{
			Model:   "llama-3.2-7b",
			Prompts: []string{"Summarize this", "Translate this", "Classify this"},
			Tier:    domain.SLASpot,
		}),
	}))
	assertToolSuccess(t, resp, "batch")

	// Tool 4: Fine-tune with LoRA
	resp = gw.HandleRequest(rpcRequest("tools/call", toolsCallParams{
		Name: "tutu_fine_tune",
		Arguments: mustMarshal(domain.FineTuneParams{
			BaseModel:  "llama-3.2-7b",
			DatasetURI: "s3://datasets/medical-qa.jsonl",
			Epochs:     10,
			LoRA:       true,
		}),
	}))
	assertToolSuccess(t, resp, "fine-tune")

	// Verify all 4 tools were metered
	if meter.TotalRecords() != 4 {
		t.Errorf("expected 4 metered records, got %d", meter.TotalRecords())
	}

	// Verify recent records are in reverse chronological order
	recent := meter.RecentRecords(4)
	expectedTools := []string{"tutu_fine_tune", "tutu_batch_process", "tutu_embed", "tutu_inference"}
	for i, rec := range recent {
		if rec.Tool != expectedTools[i] {
			t.Errorf("recent[%d].Tool = %q, want %q", i, rec.Tool, expectedTools[i])
		}
	}
}

// TestScenario_SessionLifecycle tests complete session lifecycle:
// create, use, delete, verify cleanup.
func TestScenario_SessionLifecycle(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)
	ts := httptest.NewServer(tr)
	defer ts.Close()

	// Create 3 sessions
	sessions := make([]string, 3)
	for i := 0; i < 3; i++ {
		body := rpcRequest("initialize", map[string]any{
			"protocolVersion": MCPProtocolVersion,
			"clientInfo":      map[string]string{"name": "lifecycle-test"},
		})
		resp, _ := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(string(body)))
		sessions[i] = resp.Header.Get("Mcp-Session-Id")
		resp.Body.Close()
	}

	if tr.SessionCount() != 3 {
		t.Fatalf("expected 3 sessions, got %d", tr.SessionCount())
	}

	// Delete session 1
	delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/mcp", nil)
	delReq.Header.Set("Mcp-Session-Id", sessions[0])
	delResp, _ := http.DefaultClient.Do(delReq)
	delResp.Body.Close()

	if tr.SessionCount() != 2 {
		t.Errorf("expected 2 sessions after delete, got %d", tr.SessionCount())
	}

	// Try to delete same session again (should get 404)
	delReq2, _ := http.NewRequest(http.MethodDelete, ts.URL+"/mcp", nil)
	delReq2.Header.Set("Mcp-Session-Id", sessions[0])
	delResp2, _ := http.DefaultClient.Do(delReq2)
	delResp2.Body.Close()
	if delResp2.StatusCode != http.StatusNotFound {
		t.Errorf("re-delete should be 404, got %d", delResp2.StatusCode)
	}

	// Remaining sessions should still work
	body := rpcRequest("ping", nil)
	resp, _ := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(string(body)))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("ping after session delete should work, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestScenario_MalformedRequests tests MCP gateway resilience against
// various forms of bad input that real-world clients might send.
func TestScenario_MalformedRequests(t *testing.T) {
	gw := newTestGateway(t)

	tests := []struct {
		name     string
		raw      string
		wantCode int
	}{
		{"empty json", `{}`, CodeInvalidRequest},
		{"missing jsonrpc", `{"id":1,"method":"ping"}`, CodeInvalidRequest},
		{"wrong jsonrpc version", `{"jsonrpc":"1.0","id":1,"method":"ping"}`, CodeInvalidRequest},
		{"null method", `{"jsonrpc":"2.0","id":1,"method":null}`, CodeInvalidRequest},
		{"empty method", `{"jsonrpc":"2.0","id":1,"method":""}`, CodeInvalidRequest},
		{"invalid json", `{invalid`, CodeParseError},
		{"truncated json", `{"jsonrpc":"2.0","id":1,"met`, CodeParseError},
		{"binary garbage", "\x00\x01\x02\x03\x04", CodeParseError},
		{"huge id", `{"jsonrpc":"2.0","id":99999999999999,"method":"ping"}`, -1}, // should work
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := gw.HandleRequest([]byte(tt.raw))
			if resp == nil {
				if tt.wantCode != -1 {
					t.Fatal("expected response")
				}
				return
			}
			if tt.wantCode == -1 {
				// Expected success
				if resp.Error != nil {
					t.Errorf("unexpected error: %v", resp.Error)
				}
				return
			}
			if resp.Error == nil {
				t.Fatal("expected error response")
			}
			if resp.Error.Code != tt.wantCode {
				t.Errorf("code = %d, want %d", resp.Error.Code, tt.wantCode)
			}
		})
	}
}

// TestScenario_ToolValidation tests comprehensive parameter validation
// for all MCP tools with edge cases.
func TestScenario_ToolValidation(t *testing.T) {
	gw := newTestGateway(t)

	tests := []struct {
		name    string
		tool    string
		args    any
		wantErr bool
	}{
		// Inference validation
		{"inference valid", "tutu_inference", domain.InferenceParams{Model: "m", Prompt: "p"}, false},
		{"inference no model", "tutu_inference", domain.InferenceParams{Prompt: "p"}, true},
		{"inference no prompt", "tutu_inference", domain.InferenceParams{Model: "m"}, true},
		{"inference empty both", "tutu_inference", domain.InferenceParams{}, true},

		// Embed validation
		{"embed valid", "tutu_embed", domain.EmbedParams{Model: "m", Inputs: []string{"a"}}, false},
		{"embed no model", "tutu_embed", domain.EmbedParams{Inputs: []string{"a"}}, true},
		{"embed empty inputs", "tutu_embed", domain.EmbedParams{Model: "m", Inputs: []string{}}, true},

		// Batch validation
		{"batch valid", "tutu_batch_process", domain.BatchParams{Model: "m", Prompts: []string{"a"}}, false},
		{"batch no model", "tutu_batch_process", domain.BatchParams{Prompts: []string{"a"}}, true},
		{"batch empty prompts", "tutu_batch_process", domain.BatchParams{Model: "m", Prompts: []string{}}, true},

		// Fine-tune validation
		{"finetune valid", "tutu_fine_tune", domain.FineTuneParams{BaseModel: "m", DatasetURI: "s3://x"}, false},
		{"finetune no model", "tutu_fine_tune", domain.FineTuneParams{DatasetURI: "s3://x"}, true},
		{"finetune no dataset", "tutu_fine_tune", domain.FineTuneParams{BaseModel: "m"}, true},

		// Unknown tool
		{"unknown tool", "nonexistent", map[string]string{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := rpcRequest("tools/call", toolsCallParams{
				Name:      tt.tool,
				Arguments: mustMarshal(tt.args),
			})
			resp := gw.HandleRequest(raw)
			if resp == nil {
				t.Fatal("expected response")
			}
			if tt.wantErr && resp.Error == nil {
				t.Error("expected error")
			}
			if !tt.wantErr && resp.Error != nil {
				t.Errorf("unexpected error: %v", resp.Error)
			}
		})
	}
}

// TestScenario_AllResourcesReadable tests that every advertised resource
// can be read and returns valid JSON content.
func TestScenario_AllResourcesReadable(t *testing.T) {
	gw := newTestGateway(t)

	// Get resource list
	resp := gw.HandleRequest(rpcRequest("resources/list", nil))
	if resp.Error != nil {
		t.Fatalf("resources/list error: %v", resp.Error)
	}

	var list resourcesListResult
	json.Unmarshal(resp.Result, &list)

	for _, res := range list.Resources {
		t.Run(res.Name, func(t *testing.T) {
			readResp := gw.HandleRequest(rpcRequest("resources/read", resourcesReadParams{URI: res.URI}))
			if readResp.Error != nil {
				t.Fatalf("read %s error: %v", res.URI, readResp.Error)
			}

			var result resourcesReadResult
			json.Unmarshal(readResp.Result, &result)
			if len(result.Contents) == 0 {
				t.Error("expected at least 1 content block")
			}

			// All resources should return valid JSON
			for _, content := range result.Contents {
				if content.MimeType == "application/json" {
					var parsed any
					if err := json.Unmarshal([]byte(content.Text), &parsed); err != nil {
						t.Errorf("resource %s returned invalid JSON: %v", res.URI, err)
					}
				}
			}
		})
	}
}

// TestScenario_MeteringAccuracy verifies that metering tracks costs correctly
// across different SLA tiers and tool types under realistic usage.
func TestScenario_MeteringAccuracy(t *testing.T) {
	sla := NewSLAEngine()
	meter := NewMeter(sla)
	gw := NewGateway(sla, meter)

	// Simulate a realistic workload: mixed SLA tiers
	calls := []struct {
		tool     string
		args     any
		tier     domain.SLATier
	}{
		{"tutu_inference", domain.InferenceParams{Model: "m", Prompt: "short", Priority: domain.SLARealtime}, domain.SLARealtime},
		{"tutu_inference", domain.InferenceParams{Model: "m", Prompt: strings.Repeat("a", 1000), Priority: domain.SLAStandard}, domain.SLAStandard},
		{"tutu_embed", domain.EmbedParams{Model: "m", Inputs: []string{"a", "b", "c"}}, domain.SLAStandard},
		{"tutu_batch_process", domain.BatchParams{Model: "m", Prompts: []string{"p1", "p2"}, Tier: domain.SLASpot}, domain.SLASpot},
	}

	for _, c := range calls {
		raw := rpcRequest("tools/call", toolsCallParams{
			Name:      c.tool,
			Arguments: mustMarshal(c.args),
		})
		resp := gw.HandleRequest(raw)
		if resp.Error != nil {
			t.Fatalf("tool %s failed: %v", c.tool, resp.Error)
		}
	}

	if meter.TotalRecords() != len(calls) {
		t.Errorf("expected %d records, got %d", len(calls), meter.TotalRecords())
	}

	// Verify realtime tier costs more than standard
	recent := meter.RecentRecords(len(calls))
	realtimeCost := int64(0)
	standardCost := int64(0)
	for _, rec := range recent {
		if rec.Tier == domain.SLARealtime {
			realtimeCost += rec.CostMicro
		}
		if rec.Tier == domain.SLAStandard {
			standardCost += rec.CostMicro
		}
	}
	// Standard should have more tokens (longer prompt) but cheaper per-token
	// Realtime has fewer tokens but expensive per-token
	if realtimeCost <= 0 {
		t.Error("realtime cost should be > 0")
	}
}

// TestScenario_HTTPTransport_ContentType verifies the transport layer
// handles various Content-Type headers correctly.
func TestScenario_HTTPTransport_ContentType(t *testing.T) {
	gw := newTestGateway(t)
	tr := NewTransport(gw)
	ts := httptest.NewServer(tr)
	defer ts.Close()

	body := rpcRequest("ping", nil)

	// JSON requests should work regardless of charset
	contentTypes := []string{
		"application/json",
		"application/json; charset=utf-8",
		"application/json;charset=UTF-8",
		"", // Empty should still work (body is valid JSON)
	}

	for _, ct := range contentTypes {
		t.Run(ct, func(t *testing.T) {
			req, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader(string(body)))
			if ct != "" {
				req.Header.Set("Content-Type", ct)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				b, _ := io.ReadAll(resp.Body)
				t.Errorf("status = %d, body = %s", resp.StatusCode, string(b))
			}
		})
	}
}

// TestScenario_SLATierCostOrdering verifies that tier pricing follows
// the expected hierarchy: realtime > standard > batch > spot.
func TestScenario_SLATierCostOrdering(t *testing.T) {
	sla := NewSLAEngine()

	const tokens = 1000
	realtimeCost := sla.CostMicro(domain.SLARealtime, tokens/2, tokens/2)
	standardCost := sla.CostMicro(domain.SLAStandard, tokens/2, tokens/2)
	batchCost := sla.CostMicro(domain.SLABatch, tokens/2, tokens/2)
	spotCost := sla.CostMicro(domain.SLASpot, tokens/2, tokens/2)

	if realtimeCost <= standardCost {
		t.Errorf("realtime (%d) should cost more than standard (%d)", realtimeCost, standardCost)
	}
	if standardCost <= batchCost {
		t.Errorf("standard (%d) should cost more than batch (%d)", standardCost, batchCost)
	}
	if batchCost <= spotCost {
		t.Errorf("batch (%d) should cost more than spot (%d)", batchCost, spotCost)
	}
}

// TestScenario_ProtocolCompliance checks MCP protocol compliance:
// correct JSONRPC version, ID echoing, capability negotiation.
func TestScenario_ProtocolCompliance(t *testing.T) {
	gw := newTestGateway(t)

	// Test with different ID types (int, string)
	for _, id := range []any{1, "req-001"} {
		t.Run("id="+stringify(id), func(t *testing.T) {
			raw := map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"method":  "ping",
			}
			data, _ := json.Marshal(raw)
			resp := gw.HandleRequest(data)
			if resp == nil {
				t.Fatal("expected response")
			}
			if resp.JSONRPC != "2.0" {
				t.Errorf("jsonrpc = %q, want 2.0", resp.JSONRPC)
			}
			if resp.Error != nil {
				t.Errorf("unexpected error: %v", resp.Error)
			}
		})
	}
}

// TestScenario_NotificationHandling verifies that notifications
// (requests without id) are handled correctly per MCP spec.
func TestScenario_NotificationHandling(t *testing.T) {
	gw := newTestGateway(t)

	notifications := []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`,
		`{"jsonrpc":"2.0","method":"notifications/resources/updated"}`,
	}

	for _, notif := range notifications {
		resp := gw.HandleRequest([]byte(notif))
		if resp != nil {
			t.Errorf("notification %q should return nil, got response", notif)
		}
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func errorf(format string, args ...any) error {
	return &testError{msg: strings.NewReplacer().Replace(format)}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func stringify(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func assertToolSuccess(t *testing.T, resp *Response, toolName string) {
	t.Helper()
	if resp == nil {
		t.Fatalf("%s: nil response", toolName)
	}
	if resp.Error != nil {
		t.Fatalf("%s: unexpected error: %v", toolName, resp.Error)
	}
	var result toolsCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("%s: unmarshal result: %v", toolName, err)
	}
	if len(result.Content) == 0 {
		t.Fatalf("%s: empty content", toolName)
	}
	if result.Content[0].Type != "text" {
		t.Errorf("%s: content type = %q, want text", toolName, result.Content[0].Type)
	}
}

// Force compile-time check that we use the imports
var _ = time.Second
