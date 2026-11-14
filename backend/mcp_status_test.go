package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMCPStatusClientAndAnswer(t *testing.T) {
	assigned, _ := samplePlans()
	step := Step{ID: "backend-access", Resource: "backend-staging", ActionType: "request_access"}
	plan := Plan{PlanID: assigned.PlanID, Position: assigned.Position, Role: assigned.Role,
		Team: assigned.Team, Steps: []Step{step}}
	called := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct { Method string `json:"method"`; ID int `json:"id"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil { t.Error(err) }
		if r.Header.Get("MCP-Protocol-Version") != "2025-06-18" { t.Error("missing MCP version") }
		called++
		if req.Method == "notifications/initialized" { w.WriteHeader(http.StatusAccepted); return }
		var result any
		switch req.Method {
		case "initialize": result = map[string]string{"protocolVersion":"2025-06-18"}
		case "tools/list": result = map[string]any{"tools":[]any{map[string]string{"name":"get_request_status"}}}
		case "tools/call": result = map[string]any{"structuredContent":MCPStatus{InternID:demoInternID, PlanID:plan.PlanID, StepID:step.ID, Resource:step.Resource, Status:"not_created"}}
		default: t.Errorf("unexpected method: %s", req.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc":"2.0", "id":req.ID, "result":result})
	}))
	defer server.Close()
	status, err := NewMCPStatusClient(server.URL).GetStatus(t.Context(), plan, step)
	if err != nil || called != 4 || status.Status != "not_created" { t.Fatalf("MCP client: %+v, %v, calls=%d", status, err, called) }
	answer := statusAnswer(plan, step, status)
	if answer.ToolCall != "get_request_status" || answer.Model != "" || answer.Answer == "" { t.Fatalf("wrong tool answer: %+v", answer) }
}
