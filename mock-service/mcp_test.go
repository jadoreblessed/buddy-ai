package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestMCPStatusTool(t *testing.T) {
	dir := t.TempDir()
	s, err := newService(filepath.Join(dir, "request.json"), filepath.Join(dir, "intern.json"))
	if err != nil { t.Fatal(err) }
	call := func(method string, params any) map[string]json.RawMessage {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"jsonrpc":"2.0", "id":1, "method":method, "params":params})
		r := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("MCP-Protocol-Version", "2025-06-18")
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, r)
		if w.Code != http.StatusOK { t.Fatalf("MCP %s: HTTP %d: %s", method, w.Code, w.Body.String()) }
		var response map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil { t.Fatal(err) }
		return response
	}
	if result := call("initialize", map[string]any{"protocolVersion":"2025-06-18"}); len(result["result"]) == 0 { t.Fatal("no handshake") }
	if result := call("tools/list", map[string]any{}); !bytes.Contains(result["result"], []byte("get_request_status")) { t.Fatal("tool missing") }
	plan := s.plans[s.position]
	var step Step
	for _, candidate := range plan.Steps { if candidate.ActionType == "request_access" { step = candidate } }
	params := map[string]any{"name":"get_request_status", "arguments":map[string]string{"internId":internID, "planId":plan.PlanID, "stepId":step.ID}}
	response := call("tools/call", params)
	if !bytes.Contains(response["result"], []byte(`"not_created"`)) { t.Fatalf("unexpected result: %s", response["result"]) }
	params["arguments"] = map[string]string{"internId":internID, "planId":"other", "stepId":step.ID}
	response = call("tools/call", params)
	if !bytes.Contains(response["result"], []byte(`"isError":true`)) { t.Fatalf("unassigned plan exposed: %s", response["result"]) }
}
