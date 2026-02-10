package domain

import "time"

// ─── MCP Domain Types ───────────────────────────────────────────────────────
// Architecture Part XII: MCP Server Gateway — Enterprise & AI-Giant Integration
// Protocol: MCP 2025-03-26 (JSON-RPC 2.0 over Streamable HTTP)

// ─── SLA Tiers ──────────────────────────────────────────────────────────────

// SLATier defines the quality-of-service level for an MCP client.
type SLATier string

const (
	SLARealtime SLATier = "realtime" // p99 < 200ms, $2.00/M tokens
	SLAStandard SLATier = "standard" // p99 < 2s,    $0.50/M tokens
	SLABatch    SLATier = "batch"    // p99 < 30s,   $0.10/M tokens
	SLASpot     SLATier = "spot"     // best-effort, $0.02/M tokens
)

// SLAConfig holds the pricing and performance guarantees for a tier.
type SLAConfig struct {
	Tier            SLATier       `json:"tier"`
	MaxLatencyP99   time.Duration `json:"max_latency_p99"`
	TargetTokensSec int           `json:"target_tokens_sec"`
	AvailabilityPct float64       `json:"availability_pct"` // e.g. 99.9
	PricePerMTokens float64       `json:"price_per_m_tokens"`
	Priority        int           `json:"priority"` // Task queue priority (1-255)
	MaxConcurrent   int           `json:"max_concurrent"`
	RateLimitRPM    int           `json:"rate_limit_rpm"` // Requests per minute
}

// ─── MCP Client ─────────────────────────────────────────────────────────────

// MCPClient represents an authenticated client connecting via MCP.
type MCPClient struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	SLA       SLATier `json:"sla"`
	APIKeyHex string  `json:"-"` // Never serialized
	CreatedAt int64   `json:"created_at"`
	Enabled   bool    `json:"enabled"`
}

// ─── Tool Definitions ───────────────────────────────────────────────────────

// MCPTool represents an MCP tool definition exposed to clients.
type MCPTool struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	InputSchema MCPToolInputSchema  `json:"inputSchema"`
}

// MCPToolInputSchema is the JSON Schema for tool inputs.
type MCPToolInputSchema struct {
	Type       string                     `json:"type"` // always "object"
	Properties map[string]MCPSchemaProperty `json:"properties"`
	Required   []string                   `json:"required"`
}

// MCPSchemaProperty defines a single property in a JSON Schema.
type MCPSchemaProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
}

// ─── Resource Definitions ───────────────────────────────────────────────────

// MCPResource represents an MCP resource definition exposed to clients.
type MCPResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

// MCPResourceContent is a single content block returned for a resource.
type MCPResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
}

// ─── Tool Call Types ────────────────────────────────────────────────────────

// InferenceParams are the arguments for the tutu_inference tool.
type InferenceParams struct {
	Model    string  `json:"model"`
	Prompt   string  `json:"prompt"`
	Stream   bool    `json:"stream"`
	Priority SLATier `json:"priority"`
	MaxToks  int     `json:"max_tokens"`
}

// EmbedParams are the arguments for the tutu_embed tool.
type EmbedParams struct {
	Model  string   `json:"model"`
	Inputs []string `json:"inputs"`
}

// BatchParams are the arguments for the tutu_batch_process tool.
type BatchParams struct {
	Model   string   `json:"model"`
	Prompts []string `json:"prompts"`
	Tier    SLATier  `json:"tier"`
}

// FineTuneParams are the arguments for the tutu_fine_tune tool.
type FineTuneParams struct {
	BaseModel  string `json:"base_model"`
	DatasetURI string `json:"dataset_uri"`
	Epochs     int    `json:"epochs"`
	LoRA       bool   `json:"lora"`
}

// ─── Usage Metering ─────────────────────────────────────────────────────────

// UsageRecord captures a single metered API call.
type UsageRecord struct {
	ClientID   string    `json:"client_id"`
	Tool       string    `json:"tool"`
	Model      string    `json:"model"`
	InputToks  int       `json:"input_tokens"`
	OutputToks int       `json:"output_tokens"`
	LatencyMs  int64     `json:"latency_ms"`
	Tier       SLATier   `json:"tier"`
	CostMicro  int64     `json:"cost_micro"` // Cost in microdollars (1e-6 USD)
	Timestamp  time.Time `json:"timestamp"`
}

// ClientUsageSummary aggregates usage over a time period.
type ClientUsageSummary struct {
	ClientID    string  `json:"client_id"`
	TotalCalls  int64   `json:"total_calls"`
	TotalInput  int64   `json:"total_input_tokens"`
	TotalOutput int64   `json:"total_output_tokens"`
	TotalCost   float64 `json:"total_cost_usd"`
	PeriodStart int64   `json:"period_start"`
	PeriodEnd   int64   `json:"period_end"`
}
