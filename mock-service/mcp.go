package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// A small stateless Streamable HTTP MCP endpoint for one read-only demo tool.
func (s *Service) handleMCP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Origin") != "" {
		http.Error(w, "browser origins are not allowed", http.StatusForbidden)
		return
	}
	if r.Header.Get("MCP-Protocol-Version") != "" && r.Header.Get("MCP-Protocol-Version") != "2025-06-18" {
		http.Error(w, "unsupported MCP protocol version", http.StatusBadRequest)
		return
	}
	if !strings.Contains(r.Header.Get("Accept"), "application/json") || !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		http.Error(w, "missing Accept", http.StatusNotAcceptable)
		return
	}
	var call struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&call); err != nil || call.JSONRPC != "2.0" {
		http.Error(w, "invalid JSON-RPC request", http.StatusBadRequest)
		return
	}
	if call.Method == "notifications/initialized" && len(call.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if len(call.ID) == 0 {
		http.Error(w, "missing request id", http.StatusBadRequest)
		return
	}
	var result any
	var rpcError any
	switch call.Method {
	case "initialize":
		var input struct { ProtocolVersion string `json:"protocolVersion"` }
		if json.Unmarshal(call.Params, &input) != nil || input.ProtocolVersion != "2025-06-18" {
			rpcError = map[string]any{"code": -32602, "message": "unsupported protocol version"}
			break
		}
		result = map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]string{"name": "buddy-demo-tickets", "version": "1.0.0"},
		}
	case "tools/list":
		result = map[string]any{"tools": []any{map[string]any{
			"name": "get_request_status",
			"description": "Read the current access request status for a demo intern and assigned plan step.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"internId": map[string]string{"type": "string"},
				"planId": map[string]string{"type": "string"},
				"stepId": map[string]string{"type": "string"},
			}, "required": []string{"internId", "planId", "stepId"}, "additionalProperties": false},
			"annotations": map[string]bool{"readOnlyHint": true},
		}}}
	case "tools/call":
		var input struct {
			Name string `json:"name"`
			Arguments struct {
				InternID string `json:"internId"`
				PlanID string `json:"planId"`
				StepID string `json:"stepId"`
			} `json:"arguments"`
		}
		if json.Unmarshal(call.Params, &input) != nil || input.Name != "get_request_status" {
			rpcError = map[string]any{"code": -32602, "message": "invalid tool call"}
			break
		}
		s.mu.Lock()
		plan := s.plans[s.position]
		step, ok := s.findStep(input.Arguments.PlanID, input.Arguments.StepID)
		valid := ok && input.Arguments.InternID == internID && plan.PlanID == input.Arguments.PlanID && step.ActionType == "request_access"
		var current *Request
		if valid {
			for _, request := range s.requests {
				if request.InternID == internID && request.PlanID == plan.PlanID && request.StepID == step.ID {
					copy := *request
					current = &copy
					break
				}
			}
		}
		s.mu.Unlock()
		if !valid {
			result = map[string]any{"isError": true, "content": []any{map[string]string{"type": "text", "text": "unknown assigned access step"}}}
			break
		}
		status := map[string]any{"internId": internID, "planId": plan.PlanID, "stepId": step.ID, "resource": step.Resource, "status": "not_created"}
		if current != nil {
			status["status"] = current.Status
			status["requestId"] = current.ID
		}
		data, _ := json.Marshal(status)
		result = map[string]any{"content": []any{map[string]string{"type": "text", "text": string(data)}}, "structuredContent": status}
	default:
		rpcError = map[string]any{"code": -32601, "message": "method not found"}
	}
	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
	if rpcError != nil {
		response["error"] = rpcError
	} else {
		response["result"] = result
	}
	_ = json.NewEncoder(w).Encode(response)
}
