package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAIClientGenerate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/responses" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type = %q", got)
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		for key, expected := range map[string]string{
			"model": `"gpt-5.6"`, "instructions": `"Be helpful"`, "input": `"Hello"`,
			"max_output_tokens": `350`, "store": `false`, "reasoning": `{"effort":"none"}`,
		} {
			if string(body[key]) != expected {
				t.Errorf("%s = %s, want %s", key, body[key], expected)
			}
		}
		if len(body) != 6 {
			t.Errorf("unexpected request fields: %v", body)
		}
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"reasoning"},{"type":"message","content":[{"type":"output_text","text":"  Hello"},{"type":"output_text","text":" there  "}]}]}`))
	}))
	defer server.Close()
	client := NewOpenAIClient(server.URL+"/", "gpt-5.6", "test-key")
	got, err := client.Generate(context.Background(), "Be helpful", "Hello")
	if err != nil || got != "Hello there" {
		t.Fatalf("Generate = %q, %v", got, err)
	}
}

func TestOpenAIClientFailureResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"rate limit", http.StatusTooManyRequests, `{"error":"limited"}`, ErrAIRateLimited},
		{"provider error", http.StatusInternalServerError, `{"error":"failure"}`, nil},
		{"malformed", http.StatusOK, `{`, nil},
		{"trailing json", http.StatusOK, `{"status":"completed","output":[]} {}`, nil},
		{"incomplete", http.StatusOK, `{"status":"incomplete","output":[{"type":"message","content":[{"type":"output_text","text":"partial"}]}]}`, nil},
		{"empty", http.StatusOK, `{"status":"completed","output":[]}`, nil},
		{"wrong content", http.StatusOK, `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","text":"not an answer"}]}]}`, nil},
		{"oversized", http.StatusOK, strings.Repeat("x", maxAIResponseBytes+1), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			got, err := NewOpenAIClient(server.URL, "gpt-5.6", "test-key").Generate(context.Background(), "instruction", "input")
			if got != "" || err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("Generate = %q, %v; want empty answer and error %v", got, err, tc.want)
			}
		})
	}
}

func TestOpenAIClientUnconfiguredAndTimeout(t *testing.T) {
	client := NewOpenAIClient("https://example.invalid", "gpt-5.6", "")
	if got, err := client.Generate(context.Background(), "", ""); got != "" || !errors.Is(err, ErrAIUnconfigured) {
		t.Fatalf("unconfigured Generate = %q, %v", got, err)
	}
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)
	client = NewOpenAIClient(server.URL, "gpt-5.6", "test-key")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	got, err := client.Generate(ctx, "", "")
	if got != "" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed out Generate = %q, %v", got, err)
	}
}
