package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type TicketClient struct {
	baseURL string
	client  *http.Client
}

func NewTicketClient(baseURL string) *TicketClient {
	return &TicketClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *TicketClient) call(ctx context.Context, method, path, user string, body io.Reader) (*Request, int, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, 0, err
	}
	if user != "" {
		request.Header.Set("X-Demo-User", user)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, response.StatusCode, fmt.Errorf("сервис заявок вернул HTTP %d", response.StatusCode)
	}
	var result struct {
		Request *Request `json:"request"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, response.StatusCode, fmt.Errorf("прочитать ответ сервиса заявок: %w", err)
	}
	return result.Request, response.StatusCode, nil
}

func (c *TicketClient) GetCurrent(ctx context.Context, internID, planID, stepID string) (*Request, error) {
	query := url.Values{
		"internId": {internID},
		"planId":   {planID},
		"stepId":   {stepID},
	}
	request, _, err := c.call(ctx, http.MethodGet, "/requests/current?"+query.Encode(), "", nil)
	return request, err
}

func (c *TicketClient) Create(ctx context.Context, user, planID, stepID, resource string) (*Request, int, error) {
	body, err := json.Marshal(struct {
		PlanID   string `json:"planId"`
		StepID   string `json:"stepId"`
		Resource string `json:"resource"`
	}{planID, stepID, resource})
	if err != nil {
		return nil, 0, err
	}
	request, status, err := c.call(ctx, http.MethodPost, "/requests", user, bytes.NewReader(body))
	if err == nil && request == nil {
		err = fmt.Errorf("сервис заявок не вернул заявку")
	}
	return request, status, err
}

func (c *TicketClient) Approve(ctx context.Context, user, id string) (*Request, int, error) {
	request, status, err := c.call(ctx, http.MethodPost, "/requests/"+url.PathEscape(id)+"/approve", user, nil)
	if err == nil && request == nil {
		err = fmt.Errorf("сервис заявок не вернул заявку")
	}
	return request, status, err
}
