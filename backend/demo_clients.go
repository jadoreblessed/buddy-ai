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

// DemoServiceClient is an HTTP boundary to the synthetic mock service. X-Demo-User
// selects a demo role only; it is not authentication.
type DemoServiceClient struct {
	baseURL string
	client  *http.Client
}

func newDemoServiceClient(baseURL string) DemoServiceClient {
	return DemoServiceClient{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{Timeout: 5 * time.Second}}
}

type upstreamError struct {
	status int
	err    error
}

func (e *upstreamError) Error() string { return e.err.Error() }

func (c *DemoServiceClient) call(ctx context.Context, method, path, user string, input, output any) (int, error) {
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, err
	}
	if user != "" {
		request.Header.Set("X-Demo-User", user)
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, &upstreamError{response.StatusCode, fmt.Errorf("демо-сервис вернул HTTP %d", response.StatusCode)}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output); err != nil {
		return response.StatusCode, fmt.Errorf("прочитать ответ демо-сервиса: %w", err)
	}
	return response.StatusCode, nil
}

type AbsenceRequest struct {
	ID         string `json:"id"`
	InternID   string `json:"internId"`
	Date       string `json:"date"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
	ApprovedAt string `json:"approvedAt,omitempty"`
}

type AbsenceClient struct{ DemoServiceClient }

func NewAbsenceClient(baseURL string) *AbsenceClient {
	return &AbsenceClient{newDemoServiceClient(baseURL)}
}

func validAbsence(req AbsenceRequest, internID string) bool {
	return req.ID != "" && req.InternID == internID && validUTCDate(req.Date) && req.CreatedAt != "" &&
		(req.Status == "pending" || req.Status == "approved") && (req.Status != "approved" || req.ApprovedAt != "")
}

func (c *AbsenceClient) List(ctx context.Context, internID string) ([]AbsenceRequest, error) {
	var output struct {
		Requests []AbsenceRequest `json:"requests"`
	}
	_, err := c.call(ctx, http.MethodGet, "/absences?"+url.Values{"internId": {internID}}.Encode(), internID, nil, &output)
	if err != nil {
		return nil, err
	}
	if output.Requests == nil {
		return nil, fmt.Errorf("демо-сервис не вернул список отгулов")
	}
	for _, req := range output.Requests {
		if !validAbsence(req, internID) {
			return nil, fmt.Errorf("демо-сервис вернул некорректный отгул")
		}
	}
	return output.Requests, nil
}

func (c *AbsenceClient) Create(ctx context.Context, internID, date string) (*AbsenceRequest, int, error) {
	var output struct {
		Request *AbsenceRequest `json:"request"`
	}
	status, err := c.call(ctx, http.MethodPost, "/absences", internID, struct {
		Date string `json:"date"`
	}{date}, &output)
	if err != nil {
		return nil, status, err
	}
	if (status != http.StatusCreated && status != http.StatusOK) || output.Request == nil ||
		!validAbsence(*output.Request, internID) || output.Request.Date != date {
		return nil, status, fmt.Errorf("демо-сервис вернул некорректную заявку на отгул")
	}
	return output.Request, status, nil
}

func (c *AbsenceClient) Approve(ctx context.Context, mentorID, internID, id string) (*AbsenceRequest, int, error) {
	var output struct {
		Request *AbsenceRequest `json:"request"`
	}
	status, err := c.call(ctx, http.MethodPost, "/absences/"+url.PathEscape(id)+"/approve", mentorID, nil, &output)
	if err != nil {
		return nil, status, err
	}
	if status != http.StatusOK || output.Request == nil || !validAbsence(*output.Request, internID) ||
		output.Request.ID != id || output.Request.Status != "approved" {
		return nil, status, fmt.Errorf("демо-сервис вернул некорректное согласование отгула")
	}
	return output.Request, status, nil
}

type Message struct {
	ID        string `json:"id"`
	InternID  string `json:"internId"`
	MentorID  string `json:"mentorId"`
	PlanID    string `json:"planId"`
	Text      string `json:"text"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

type MessageClient struct{ DemoServiceClient }

func NewMessageClient(baseURL string) *MessageClient {
	return &MessageClient{newDemoServiceClient(baseURL)}
}

func validMessage(message Message, internID, mentorID, planID string) bool {
	return message.ID != "" && message.InternID == internID && message.MentorID == mentorID &&
		message.PlanID == planID && message.Text != "" && message.Status == "sent" && message.CreatedAt != ""
}

func (c *MessageClient) List(ctx context.Context, internID, mentorID, planID string) ([]Message, error) {
	query := url.Values{"internId": {internID}, "mentorId": {mentorID}, "planId": {planID}}
	var output struct {
		Messages []Message `json:"messages"`
	}
	_, err := c.call(ctx, http.MethodGet, "/messages?"+query.Encode(), internID, nil, &output)
	if err != nil {
		return nil, err
	}
	if output.Messages == nil {
		return nil, fmt.Errorf("демо-сервис не вернул список сообщений")
	}
	for _, message := range output.Messages {
		if !validMessage(message, internID, mentorID, planID) {
			return nil, fmt.Errorf("демо-сервис вернул некорректное сообщение")
		}
	}
	return output.Messages, nil
}

func (c *MessageClient) Send(ctx context.Context, internID, mentorID, planID, text string) (*Message, int, error) {
	var output struct {
		Message *Message `json:"message"`
	}
	status, err := c.call(ctx, http.MethodPost, "/messages", internID,
		struct {
			MentorID string `json:"mentorId"`
			PlanID   string `json:"planId"`
			Text     string `json:"text"`
		}{mentorID, planID, text}, &output)
	if err != nil {
		return nil, status, err
	}
	if status != http.StatusCreated || output.Message == nil ||
		!validMessage(*output.Message, internID, mentorID, planID) || output.Message.Text != text {
		return nil, status, fmt.Errorf("демо-сервис вернул некорректную квитанцию сообщения")
	}
	return output.Message, status, nil
}
