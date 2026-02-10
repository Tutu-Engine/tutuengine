// Package mcp implements the TuTu MCP Gateway.
// Architecture Part XII: MCP Server Gateway — Enterprise & AI-Giant Integration
//
// Protocol: MCP 2025-03-26 over JSON-RPC 2.0 / Streamable HTTP
// Tools:    tutu_inference, tutu_embed, tutu_batch_process, tutu_fine_tune
// Resources: tutu://capacity, tutu://models, tutu://regions/{region}
package mcp

import (
	"encoding/json"
	"fmt"
)

// ─── JSON-RPC 2.0 ──────────────────────────────────────────────────────────
// Spec: https://www.jsonrpc.org/specification

// JSONRPCVersion is the only valid JSON-RPC version string.
const JSONRPCVersion = "2.0"

// Request is a JSON-RPC 2.0 request object.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"` // string | int | null
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response object.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification (no id).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// ─── Standard JSON-RPC 2.0 Error Codes ─────────────────────────────────────

const (
	CodeParseError     = -32700 // Invalid JSON
	CodeInvalidRequest = -32600 // Not a valid Request object
	CodeMethodNotFound = -32601 // Method does not exist
	CodeInvalidParams  = -32602 // Invalid method parameters
	CodeInternalError  = -32603 // Internal error
)

// MCP-specific error codes (per MCP spec).
const (
	CodeRequestCancelled = -32800 // Client cancelled the request
	CodeContentTooLarge  = -32801 // Content exceeds maximum size
)

// NewParseError creates a parse error response.
func NewParseError(id any) Response {
	return errResponse(id, CodeParseError, "Parse error")
}

// NewInvalidRequest creates an invalid request error response.
func NewInvalidRequest(id any) Response {
	return errResponse(id, CodeInvalidRequest, "Invalid Request")
}

// NewMethodNotFound creates a method-not-found error response.
func NewMethodNotFound(id any, method string) Response {
	return errResponse(id, CodeMethodNotFound, fmt.Sprintf("Method not found: %s", method))
}

// NewInvalidParams creates an invalid params error response.
func NewInvalidParams(id any, detail string) Response {
	return errResponse(id, CodeInvalidParams, fmt.Sprintf("Invalid params: %s", detail))
}

// NewInternalError creates an internal error response.
func NewInternalError(id any, detail string) Response {
	return errResponse(id, CodeInternalError, fmt.Sprintf("Internal error: %s", detail))
}

// NewResult creates a successful response with the given result.
func NewResult(id any, result any) (Response, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return Response{}, fmt.Errorf("marshal result: %w", err)
	}
	return Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  data,
	}, nil
}

// ParseRequest decodes a raw JSON message into a Request.
// Returns an error response if the message is malformed.
func ParseRequest(raw []byte) (Request, *Response) {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		resp := NewParseError(nil)
		return Request{}, &resp
	}
	if req.JSONRPC != JSONRPCVersion {
		resp := NewInvalidRequest(req.ID)
		return Request{}, &resp
	}
	if req.Method == "" {
		resp := NewInvalidRequest(req.ID)
		return Request{}, &resp
	}
	return req, nil
}

func errResponse(id any, code int, message string) Response {
	return Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}
