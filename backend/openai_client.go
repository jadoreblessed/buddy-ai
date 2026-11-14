package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	ErrAIUnconfigured = errors.New("AI is not configured")
	ErrAIRateLimited  = errors.New("AI provider rate limited")
)

const maxAIResponseBytes = 1 << 20

type OpenAIClient struct {
	baseURL string
	model   string
	key     string
	client  *http.Client
}

func NewOpenAIClient(baseURL, model, key string) *OpenAIClient {
	return &OpenAIClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		key:     key,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *OpenAIClient) Generate(ctx context.Context, instructions, input string) (string, error) {
	if c == nil || strings.TrimSpace(c.key) == "" || strings.TrimSpace(c.model) == "" || c.baseURL == "" {
		return "", ErrAIUnconfigured
	}
	payload, err := json.Marshal(struct {
		Model           string `json:"model"`
		Instructions    string `json:"instructions"`
		Input           string `json:"input"`
		MaxOutputTokens int    `json:"max_output_tokens"`
		Store           bool   `json:"store"`
		Reasoning       struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}{Model: c.model, Instructions: instructions, Input: input, MaxOutputTokens: 350, Store: false, Reasoning: struct {
		Effort string `json:"effort"`
	}{Effort: "none"}})
	if err != nil {
		return "", fmt.Errorf("encode AI request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create AI request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.key)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("AI provider unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return "", ErrAIRateLimited
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI provider returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAIResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("read AI response: %w", err)
	}
	if len(body) > maxAIResponseBytes {
		return "", errors.New("AI response too large")
	}
	var result struct {
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&result); err != nil {
		return "", fmt.Errorf("decode AI response: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", errors.New("invalid AI response trailing content")
	}
	if result.Status != "completed" {
		return "", fmt.Errorf("AI response status %q", result.Status)
	}
	var answer strings.Builder
	for _, item := range result.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" {
				answer.WriteString(content.Text)
			}
		}
	}
	text := strings.TrimSpace(answer.String())
	if text == "" {
		return "", errors.New("AI response contains no text")
	}
	return text, nil
}
