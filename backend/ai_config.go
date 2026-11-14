package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type AIConfig struct {
	BaseURL string
	Model   string
	Key     string
}

// loadAIConfig reads only the AI settings from a local .env file. Environment
// variables win, so CI and reviewers never need to edit the file.
func loadAIConfig(path string) (AIConfig, error) {
	values := make(map[string]string, 3)
	file, err := os.Open(path)
	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for line := 1; scanner.Scan(); line++ {
			text := strings.TrimSpace(scanner.Text())
			if text == "" || strings.HasPrefix(text, "#") {
				continue
			}
			key, value, ok := strings.Cut(text, "=")
			if !ok {
				return AIConfig{}, fmt.Errorf("некорректная строка %d в .env", line)
			}
			key = strings.TrimSpace(key)
			if key == "AI_API_KEY" || key == "AI_MODEL" || key == "AI_BASE_URL" {
				values[key] = strings.TrimSpace(value)
			}
		}
		if err := scanner.Err(); err != nil {
			return AIConfig{}, fmt.Errorf("прочитать .env: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return AIConfig{}, fmt.Errorf("открыть .env: %w", err)
	}
	get := func(key, fallback string) string {
		if value := os.Getenv(key); value != "" {
			return value
		}
		if value := values[key]; value != "" {
			return value
		}
		return fallback
	}
	return AIConfig{
		BaseURL: get("AI_BASE_URL", "https://api.openai.com/v1"),
		Model:   get("AI_MODEL", "gpt-5.6-luna"),
		Key:     get("AI_API_KEY", ""),
	}, nil
}
