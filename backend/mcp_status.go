package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type MCPStatusClient struct {
	url    string
	client *http.Client
}

type MCPStatus struct {
	InternID  string `json:"internId"`
	PlanID    string `json:"planId"`
	StepID    string `json:"stepId"`
	Resource  string `json:"resource"`
	Status    string `json:"status"`
	RequestID string `json:"requestId"`
}

func NewMCPStatusClient(baseURL string) *MCPStatusClient {
	return &MCPStatusClient{url: strings.TrimRight(baseURL, "/") + "/mcp", client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *MCPStatusClient) call(ctx context.Context, id int, method string, params any, result any) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	response, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("MCP HTTP %d", response.StatusCode)
	}
	var envelope struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return err
	}
	if envelope.ID != id || envelope.Error != nil || len(envelope.Result) == 0 {
		return fmt.Errorf("MCP returned an invalid response")
	}
	return json.Unmarshal(envelope.Result, result)
}

func (c *MCPStatusClient) GetStatus(ctx context.Context, plan Plan, step Step) (MCPStatus, error) {
	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	err := c.call(ctx, 1, "initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]string{"name": "buddy-ai", "version": "1.0.0"},
	}, &init)
	if err != nil {
		return MCPStatus{}, err
	}
	if init.ProtocolVersion != "2025-06-18" {
		return MCPStatus{}, fmt.Errorf("unsupported MCP version")
	}
	if err := c.initialized(ctx); err != nil {
		return MCPStatus{}, err
	}
	var listing struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := c.call(ctx, 2, "tools/list", map[string]any{}, &listing); err != nil {
		return MCPStatus{}, err
	}
	found := false
	for _, tool := range listing.Tools {
		if tool.Name == "get_request_status" {
			found = true
		}
	}
	if !found {
		return MCPStatus{}, fmt.Errorf("MCP status tool unavailable")
	}
	var output struct {
		IsError           bool      `json:"isError"`
		StructuredContent MCPStatus `json:"structuredContent"`
	}
	err = c.call(ctx, 3, "tools/call", map[string]any{"name": "get_request_status", "arguments": map[string]string{
		"internId": demoInternID, "planId": plan.PlanID, "stepId": step.ID,
	}}, &output)
	if err != nil {
		return MCPStatus{}, err
	}
	status := output.StructuredContent
	if output.IsError || status.InternID != demoInternID || status.PlanID != plan.PlanID || status.StepID != step.ID || status.Resource != step.Resource {
		return MCPStatus{}, fmt.Errorf("MCP status does not match assigned plan")
	}
	if status.Status != "not_created" && status.Status != "pending" && status.Status != "approved" {
		return MCPStatus{}, fmt.Errorf("invalid MCP status")
	}
	if status.Status == "not_created" && status.RequestID != "" || status.Status != "not_created" && status.RequestID == "" {
		return MCPStatus{}, fmt.Errorf("invalid MCP request ID")
	}
	return status, nil
}

func (c *MCPStatusClient) initialized(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url,
		strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("MCP-Protocol-Version", "2025-06-18")
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("MCP initialization HTTP %d", response.StatusCode)
	}
	return nil
}

func asksRequestStatus(question string) bool {
	q := strings.ToLower(question)
	return (strings.Contains(q, "заявк") || strings.Contains(q, "доступ")) &&
		(strings.Contains(q, "статус") || strings.Contains(q, "согласова") || strings.Contains(q, "одобри") || strings.Contains(q, "отправлен"))
}

func statusAnswer(plan Plan, step Step, status MCPStatus) BuddyAnswer {
	message := "Заявка на доступ к «" + step.Resource + "» ещё не создана. Создай её в плане адаптации."
	if status.Status == "pending" {
		message = "Заявка " + status.RequestID + " на доступ к «" + step.Resource + "» ожидает согласования наставником."
	}
	if status.Status == "approved" {
		message = "Заявка " + status.RequestID + " на доступ к «" + step.Resource + "» согласована наставником."
	}
	source := assignedPlanMaterial(plan)
	return BuddyAnswer{Answer: message + " [" + source.ID + "]", Sources: []AnswerSource{{ID: source.ID, Title: source.Title, Excerpt: source.Content}}, ToolCall: "get_request_status"}
}
