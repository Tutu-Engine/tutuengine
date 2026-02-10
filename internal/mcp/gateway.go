package mcp

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── MCP Gateway ────────────────────────────────────────────────────────────
// Architecture Part XII: Enterprise-grade MCP endpoint.
// Protocol: MCP 2025-03-26 — initialize, tools/list, tools/call,
// resources/list, resources/read
//
// The Gateway is the entry point for all MCP JSON-RPC 2.0 requests.
// It routes to tool handlers, manages SLA, and meters usage.

const (
	MCPProtocolVersion = "2025-03-26"
	ServerName         = "tutu-mcp"
	ServerVersion      = "0.3.0"
)

// Gateway is the MCP server that handles JSON-RPC 2.0 requests.
type Gateway struct {
	sla   *SLAEngine
	meter *Meter
	tools []domain.MCPTool
	resources []domain.MCPResource
}

// NewGateway creates a fully configured MCP Gateway.
func NewGateway(sla *SLAEngine, meter *Meter) *Gateway {
	g := &Gateway{
		sla:   sla,
		meter: meter,
	}
	g.tools = g.defineTools()
	g.resources = g.defineResources()
	return g
}

// HandleRequest is the main dispatch for a JSON-RPC 2.0 request.
// It returns a Response for requests, or nil for notifications.
func (g *Gateway) HandleRequest(raw []byte) *Response {
	req, errResp := ParseRequest(raw)
	if errResp != nil {
		return errResp
	}

	// Notifications have no id — no response needed.
	if req.ID == nil {
		g.handleNotification(req)
		return nil
	}

	resp := g.dispatch(req)
	return &resp
}

// dispatch routes a request to the appropriate handler.
func (g *Gateway) dispatch(req Request) Response {
	switch req.Method {
	case "initialize":
		return g.handleInitialize(req)
	case "notifications/initialized":
		// Client acknowledgment — no response needed for requests with id
		return g.ack(req.ID)
	case "tools/list":
		return g.handleToolsList(req)
	case "tools/call":
		return g.handleToolsCall(req)
	case "resources/list":
		return g.handleResourcesList(req)
	case "resources/read":
		return g.handleResourcesRead(req)
	case "ping":
		return g.ack(req.ID)
	default:
		return NewMethodNotFound(req.ID, req.Method)
	}
}

// ─── initialize ─────────────────────────────────────────────────────────────

type initializeParams struct {
	ProtocolVersion string     `json:"protocolVersion"`
	ClientInfo      clientInfo `json:"clientInfo"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type initializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	ServerInfo      serverInfo   `json:"serverInfo"`
	Capabilities    capabilities `json:"capabilities"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type capabilities struct {
	Tools     *toolsCap     `json:"tools,omitempty"`
	Resources *resourcesCap `json:"resources,omitempty"`
	Logging   *struct{}     `json:"logging,omitempty"`
}

type toolsCap struct {
	ListChanged bool `json:"listChanged"`
}

type resourcesCap struct {
	Subscribe   bool `json:"subscribe"`
	ListChanged bool `json:"listChanged"`
}

func (g *Gateway) handleInitialize(req Request) Response {
	var params initializeParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewInvalidParams(req.ID, "invalid initialize params")
		}
	}

	log.Printf("[mcp] initialize from client=%s version=%s protocol=%s",
		params.ClientInfo.Name, params.ClientInfo.Version, params.ProtocolVersion)

	result := initializeResult{
		ProtocolVersion: MCPProtocolVersion,
		ServerInfo: serverInfo{
			Name:    ServerName,
			Version: ServerVersion,
		},
		Capabilities: capabilities{
			Tools:     &toolsCap{ListChanged: true},
			Resources: &resourcesCap{Subscribe: true, ListChanged: true},
			Logging:   &struct{}{},
		},
	}

	resp, err := NewResult(req.ID, result)
	if err != nil {
		return NewInternalError(req.ID, err.Error())
	}
	return resp
}

// ─── tools/list ─────────────────────────────────────────────────────────────

type toolsListResult struct {
	Tools []domain.MCPTool `json:"tools"`
}

func (g *Gateway) handleToolsList(req Request) Response {
	result := toolsListResult{Tools: g.tools}
	resp, err := NewResult(req.ID, result)
	if err != nil {
		return NewInternalError(req.ID, err.Error())
	}
	return resp
}

// ─── tools/call ─────────────────────────────────────────────────────────────

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolsCallResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func (g *Gateway) handleToolsCall(req Request) Response {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewInvalidParams(req.ID, "invalid tools/call params")
	}

	switch params.Name {
	case "tutu_inference":
		return g.callInference(req.ID, params.Arguments)
	case "tutu_embed":
		return g.callEmbed(req.ID, params.Arguments)
	case "tutu_batch_process":
		return g.callBatch(req.ID, params.Arguments)
	case "tutu_fine_tune":
		return g.callFineTune(req.ID, params.Arguments)
	default:
		return NewInvalidParams(req.ID, fmt.Sprintf("unknown tool: %s", params.Name))
	}
}

// ─── Tool Handlers (Phase 2: Stubs that validate & meter) ───────────────────

func (g *Gateway) callInference(id any, args json.RawMessage) Response {
	var p domain.InferenceParams
	if err := json.Unmarshal(args, &p); err != nil {
		return NewInvalidParams(id, "invalid inference params")
	}
	if p.Model == "" {
		return NewInvalidParams(id, "model is required")
	}
	if p.Prompt == "" {
		return NewInvalidParams(id, "prompt is required")
	}

	tier := p.Priority
	if tier == "" {
		tier = domain.SLAStandard
	}

	// Phase 2 stub: simulate inference and meter usage
	inputToks := len(p.Prompt) / 4  // ~4 chars per token
	outputToks := 50                  // stub output length
	g.meter.Record("stub-client", "tutu_inference", p.Model, inputToks, outputToks, 42, tier)

	text := fmt.Sprintf("Inference accepted: model=%s tokens=%d tier=%s", p.Model, inputToks, tier)
	return g.toolResult(id, text)
}

func (g *Gateway) callEmbed(id any, args json.RawMessage) Response {
	var p domain.EmbedParams
	if err := json.Unmarshal(args, &p); err != nil {
		return NewInvalidParams(id, "invalid embed params")
	}
	if p.Model == "" {
		return NewInvalidParams(id, "model is required")
	}
	if len(p.Inputs) == 0 {
		return NewInvalidParams(id, "inputs must not be empty")
	}

	totalToks := 0
	for _, inp := range p.Inputs {
		totalToks += len(inp) / 4
	}
	g.meter.Record("stub-client", "tutu_embed", p.Model, totalToks, 0, 15, domain.SLAStandard)

	text := fmt.Sprintf("Embedding accepted: model=%s inputs=%d tokens=%d", p.Model, len(p.Inputs), totalToks)
	return g.toolResult(id, text)
}

func (g *Gateway) callBatch(id any, args json.RawMessage) Response {
	var p domain.BatchParams
	if err := json.Unmarshal(args, &p); err != nil {
		return NewInvalidParams(id, "invalid batch params")
	}
	if p.Model == "" {
		return NewInvalidParams(id, "model is required")
	}
	if len(p.Prompts) == 0 {
		return NewInvalidParams(id, "prompts must not be empty")
	}

	tier := p.Tier
	if tier == "" {
		tier = domain.SLABatch
	}

	totalToks := 0
	for _, pr := range p.Prompts {
		totalToks += len(pr) / 4
	}
	g.meter.Record("stub-client", "tutu_batch_process", p.Model, totalToks, totalToks, 200, tier)

	text := fmt.Sprintf("Batch accepted: model=%s prompts=%d tier=%s", p.Model, len(p.Prompts), tier)
	return g.toolResult(id, text)
}

func (g *Gateway) callFineTune(id any, args json.RawMessage) Response {
	var p domain.FineTuneParams
	if err := json.Unmarshal(args, &p); err != nil {
		return NewInvalidParams(id, "invalid fine_tune params")
	}
	if p.BaseModel == "" {
		return NewInvalidParams(id, "base_model is required")
	}
	if p.DatasetURI == "" {
		return NewInvalidParams(id, "dataset_uri is required")
	}
	if p.Epochs <= 0 {
		p.Epochs = 3
	}

	g.meter.Record("stub-client", "tutu_fine_tune", p.BaseModel, 0, 0, 0, domain.SLABatch)

	text := fmt.Sprintf("Fine-tune accepted: base=%s dataset=%s epochs=%d lora=%v",
		p.BaseModel, p.DatasetURI, p.Epochs, p.LoRA)
	return g.toolResult(id, text)
}

// ─── resources/list ─────────────────────────────────────────────────────────

type resourcesListResult struct {
	Resources []domain.MCPResource `json:"resources"`
}

func (g *Gateway) handleResourcesList(req Request) Response {
	result := resourcesListResult{Resources: g.resources}
	resp, err := NewResult(req.ID, result)
	if err != nil {
		return NewInternalError(req.ID, err.Error())
	}
	return resp
}

// ─── resources/read ─────────────────────────────────────────────────────────

type resourcesReadParams struct {
	URI string `json:"uri"`
}

type resourcesReadResult struct {
	Contents []domain.MCPResourceContent `json:"contents"`
}

func (g *Gateway) handleResourcesRead(req Request) Response {
	var params resourcesReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewInvalidParams(req.ID, "invalid resources/read params")
	}

	switch params.URI {
	case "tutu://capacity":
		return g.readCapacity(req.ID)
	case "tutu://models":
		return g.readModels(req.ID)
	default:
		return NewInvalidParams(req.ID, fmt.Sprintf("unknown resource: %s", params.URI))
	}
}

func (g *Gateway) readCapacity(id any) Response {
	// Phase 2 stub — returns synthetic capacity data
	capacity := map[string]any{
		"total_nodes":     1,
		"online_nodes":    1,
		"total_vram_gb":   0,
		"available_vram_gb": 0,
		"queued_tasks":    0,
		"active_tasks":    0,
	}
	data, _ := json.Marshal(capacity)
	result := resourcesReadResult{
		Contents: []domain.MCPResourceContent{
			{URI: "tutu://capacity", MimeType: "application/json", Text: string(data)},
		},
	}
	resp, err := NewResult(id, result)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return resp
}

func (g *Gateway) readModels(id any) Response {
	// Phase 2 stub — returns synthetic model list
	models := []map[string]any{
		{"name": "llama-3.2-1b", "parameters": "1B", "quantizations": []string{"Q4_K_M", "Q8_0"}},
		{"name": "llama-3.2-7b", "parameters": "7B", "quantizations": []string{"Q4_K_M", "Q5_K_M", "Q8_0"}},
		{"name": "llama-3.2-70b", "parameters": "70B", "quantizations": []string{"Q4_K_M"}},
	}
	data, _ := json.Marshal(models)
	result := resourcesReadResult{
		Contents: []domain.MCPResourceContent{
			{URI: "tutu://models", MimeType: "application/json", Text: string(data)},
		},
	}
	resp, err := NewResult(id, result)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return resp
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func (g *Gateway) toolResult(id any, text string) Response {
	result := toolsCallResult{
		Content: []contentBlock{{Type: "text", Text: text}},
	}
	resp, err := NewResult(id, result)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return resp
}

func (g *Gateway) ack(id any) Response {
	resp, _ := NewResult(id, struct{}{})
	return resp
}

func (g *Gateway) handleNotification(req Request) {
	log.Printf("[mcp] notification: %s", req.Method)
}

// ─── Tool & Resource Definitions ────────────────────────────────────────────

func (g *Gateway) defineTools() []domain.MCPTool {
	return []domain.MCPTool{
		{
			Name:        "tutu_inference",
			Description: "Route inference to TuTu's distributed GPU network. Supports streaming.",
			InputSchema: domain.MCPToolInputSchema{
				Type: "object",
				Properties: map[string]domain.MCPSchemaProperty{
					"model":      {Type: "string", Description: "Model name (e.g., llama-3.2-70b)"},
					"prompt":     {Type: "string", Description: "Input prompt"},
					"stream":     {Type: "boolean", Description: "Enable token streaming", Default: false},
					"priority":   {Type: "string", Description: "SLA tier", Enum: []string{"realtime", "standard", "batch", "spot"}, Default: "standard"},
					"max_tokens": {Type: "integer", Description: "Maximum tokens to generate", Default: 2048},
				},
				Required: []string{"model", "prompt"},
			},
		},
		{
			Name:        "tutu_embed",
			Description: "Generate embeddings at scale using TuTu's network.",
			InputSchema: domain.MCPToolInputSchema{
				Type: "object",
				Properties: map[string]domain.MCPSchemaProperty{
					"model":  {Type: "string", Description: "Embedding model name"},
					"inputs": {Type: "array", Description: "List of text inputs to embed"},
				},
				Required: []string{"model", "inputs"},
			},
		},
		{
			Name:        "tutu_batch_process",
			Description: "Process multiple prompts in batch with configurable SLA tier.",
			InputSchema: domain.MCPToolInputSchema{
				Type: "object",
				Properties: map[string]domain.MCPSchemaProperty{
					"model":   {Type: "string", Description: "Model name"},
					"prompts": {Type: "array", Description: "List of prompts to process"},
					"tier":    {Type: "string", Description: "SLA tier for batch", Enum: []string{"standard", "batch", "spot"}, Default: "batch"},
				},
				Required: []string{"model", "prompts"},
			},
		},
		{
			Name:        "tutu_fine_tune",
			Description: "Distributed fine-tuning across TuTu's network (LoRA supported).",
			InputSchema: domain.MCPToolInputSchema{
				Type: "object",
				Properties: map[string]domain.MCPSchemaProperty{
					"base_model":  {Type: "string", Description: "Base model to fine-tune"},
					"dataset_uri": {Type: "string", Description: "URI of training dataset"},
					"epochs":      {Type: "integer", Description: "Training epochs", Default: 3},
					"lora":        {Type: "boolean", Description: "Use LoRA adapter", Default: true},
				},
				Required: []string{"base_model", "dataset_uri"},
			},
		},
	}
}

func (g *Gateway) defineResources() []domain.MCPResource {
	return []domain.MCPResource{
		{
			URI:         "tutu://capacity",
			Name:        "Network Capacity",
			Description: "Real-time network capacity: online nodes, VRAM, queued tasks",
			MimeType:    "application/json",
		},
		{
			URI:         "tutu://models",
			Name:        "Available Models",
			Description: "Models available on the network with quantizations",
			MimeType:    "application/json",
		},
		{
			URI:         "tutu://regions/global",
			Name:        "Global Region Stats",
			Description: "Node statistics per geographic region",
			MimeType:    "application/json",
		},
	}
}
